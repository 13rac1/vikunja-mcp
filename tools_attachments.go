package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxUploadSize is the maximum file size (in bytes) that upload_attachment will
// read into memory. Files larger than this are rejected before any I/O.
const maxUploadSize = 100 * 1024 * 1024 // 100 MB

type ListAttachmentsInput struct {
	TaskID  int64 `json:"task_id"            jsonschema:"the task ID,required"`
	Page    int   `json:"page,omitempty"     jsonschema:"page number (1-based, default 1)"`
	PerPage int   `json:"per_page,omitempty" jsonschema:"items per page (default 50, max 1000)"`
}

type UploadAttachmentInput struct {
	TaskID   int64  `json:"task_id"   jsonschema:"the task ID,required"`
	FilePath string `json:"file_path" jsonschema:"absolute path to the file to upload (no symlinks),required"`
}

type DownloadAttachmentInput struct {
	TaskID       int64  `json:"task_id"       jsonschema:"the task ID,required"`
	AttachmentID int64  `json:"attachment_id" jsonschema:"the attachment ID,required"`
	OutputPath   string `json:"output_path"   jsonschema:"absolute path to write the downloaded file (must not already exist),required"`
}

type DeleteAttachmentInput struct {
	TaskID       int64 `json:"task_id"       jsonschema:"the task ID,required"`
	AttachmentID int64 `json:"attachment_id" jsonschema:"the attachment ID to delete,required"`
}

// validateAndOpenUpload checks that filePath is a clean absolute path pointing to
// a regular file that is not a symlink and within the size limit. It opens the file
// and verifies the opened fd matches the Lstat result (same device+inode) to close
// the TOCTOU race window. The caller must close the returned *os.File.
func validateAndOpenUpload(filePath string) (*os.File, error) {
	clean := filepath.Clean(filePath)
	if clean != filePath {
		return nil, fmt.Errorf("file_path must be a clean absolute path (got %s, cleaned to %s)", filePath, clean)
	}
	if !filepath.IsAbs(clean) {
		return nil, fmt.Errorf("file_path must be absolute: %s", filePath)
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return nil, fmt.Errorf("file_path: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("file_path must not be a symlink: %s", filePath)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("file_path is not a regular file: %s", filePath)
	}
	if info.Size() > maxUploadSize {
		return nil, fmt.Errorf("file too large: %d bytes (max %d)", info.Size(), maxUploadSize)
	}
	f, err := os.Open(clean)
	if err != nil {
		return nil, fmt.Errorf("opening file: %w", err)
	}
	// Verify the opened fd matches what we Lstat'd (same device+inode)
	// to detect file swaps in the race window.
	openInfo, err := f.Stat()
	if err != nil {
		f.Close() //nolint:gosec // best-effort close in error path
		return nil, fmt.Errorf("stat open file: %w", err)
	}
	if !os.SameFile(info, openInfo) {
		f.Close() //nolint:gosec // best-effort close in error path
		return nil, fmt.Errorf("file changed between validation and open: %s", filePath)
	}
	return f, nil
}

// validateDownloadPath checks that outputPath is a clean absolute path whose parent
// directory exists and that the file does not already exist. Returns the resolved
// path with symlinks in the parent chain expanded (to prevent writing through
// user-created symlinks to unexpected locations).
func validateDownloadPath(outputPath string) (string, error) {
	clean := filepath.Clean(outputPath)
	if clean != outputPath {
		return "", fmt.Errorf("output_path must be a clean absolute path (got %s, cleaned to %s)", outputPath, clean)
	}
	if !filepath.IsAbs(clean) {
		return "", fmt.Errorf("output_path must be absolute: %s", outputPath)
	}

	// Resolve symlinks in the parent directory chain so we write to the real
	// location, not through a symlink to an unexpected target.
	parentDir := filepath.Dir(clean)
	resolvedParent, err := filepath.EvalSymlinks(parentDir)
	if err != nil {
		return "", fmt.Errorf("output_path parent directory: %w", err)
	}
	resolvedPath := filepath.Join(resolvedParent, filepath.Base(clean))

	// Refuse to overwrite existing files.
	if _, err := os.Lstat(resolvedPath); err == nil {
		return "", fmt.Errorf("output_path already exists: %s", outputPath)
	}
	return resolvedPath, nil
}

func registerAttachmentTools(server *mcp.Server, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_attachments",
		Description: "List attachments on a task. Returns attachment metadata including file name, mime type, and size. Supports pagination.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListAttachmentsInput) (*mcp.CallToolResult, any, error) {
		path := appendQuery(
			fmt.Sprintf("/tasks/%d/attachments", input.TaskID),
			buildPageQuery("", input.Page, input.PerPage),
		)
		raw, err := client.doRaw(ctx, "GET", path, nil)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return filteredResult(raw, attachmentFields), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "upload_attachment",
		Description: "Upload a file as an attachment to a task. The file_path must be an absolute path to an existing regular file (not a symlink). Max 100 MB.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input UploadAttachmentInput) (*mcp.CallToolResult, any, error) {
		f, err := validateAndOpenUpload(input.FilePath)
		if err != nil {
			return errorResult(err), nil, nil
		}
		defer f.Close()

		raw, err := client.doUpload(ctx, fmt.Sprintf("/tasks/%d/attachments", input.TaskID), f, filepath.Base(input.FilePath))
		if err != nil {
			return errorResult(err), nil, nil
		}
		return filteredResult(raw, uploadResultFields), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "download_attachment",
		Description: "Download a task attachment to a local file. The output_path must be an absolute path, must not already exist, and its parent directory must exist with no symlinks.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DownloadAttachmentInput) (*mcp.CallToolResult, any, error) {
		resolvedPath, err := validateDownloadPath(input.OutputPath)
		if err != nil {
			return errorResult(err), nil, nil
		}

		apiPath := fmt.Sprintf("/tasks/%d/attachments/%d", input.TaskID, input.AttachmentID)
		if err := client.doDownload(ctx, apiPath, resolvedPath); err != nil {
			return errorResult(err), nil, nil
		}
		return textResult(fmt.Appendf(nil, `{"status":"saved","path":%q}`, resolvedPath)), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_attachment",
		Description: "Delete an attachment from a task.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DeleteAttachmentInput) (*mcp.CallToolResult, any, error) {
		err := client.do(ctx, "DELETE", fmt.Sprintf("/tasks/%d/attachments/%d", input.TaskID, input.AttachmentID), nil, nil)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return deleteResult(), nil, nil
	})
}
