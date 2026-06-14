package cmds

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// AttachCmd stores a file as a named binary attachment on an artifact.
func AttachCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "attach <id> --file <path>",
		Short: "Attach a binary file (image, PDF, …) to an artifact",
		Long: `Read a file from disk and store it as a named binary attachment on an artifact.

Attachments are returned as inline image blocks when a vision-capable agent
calls artifact(action=get), allowing the model to see diagrams, screenshots,
or any other visual content alongside the artifact's text sections.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath, _ := cmd.Flags().GetString("file")
			if filePath == "" {
				return fmt.Errorf("--file is required") //nolint:err113 // user-facing
			}
			data, err := os.ReadFile(filePath) //nolint:gosec // operator-supplied path
			if err != nil {
				return fmt.Errorf("read file: %w", err)
			}
			attachmentName := name
			if attachmentName == "" {
				attachmentName = filepath.Base(filePath)
			}
			contentType := inferContentType(filePath)

			svc, cleanup := MustService()
			defer cleanup()
			ctx := context.Background()

			if err := svc.Proto.Store().PutAttachment(ctx, args[0], attachmentName, contentType, data); err != nil {
				return err
			}
			fmt.Printf("attached %s (%s, %d bytes) to %s\n", attachmentName, contentType, len(data), args[0])
			return nil
		},
	}
	cmd.Flags().String("file", "", "path to the file to attach (required)")
	cmd.Flags().StringVar(&name, "name", "", "attachment name (default: filename)")
	return cmd
}

// DetachCmd removes a named attachment from an artifact.
func DetachCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "detach <id> <name>",
		Short: "Remove a named attachment from an artifact",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			ctx := context.Background()
			if err := svc.Proto.Store().DeleteAttachment(ctx, args[0], args[1]); err != nil {
				return err
			}
			fmt.Printf("detached %s from %s\n", args[1], args[0])
			return nil
		},
	}
}

// AttachmentsCmd lists all attachments for an artifact.
func AttachmentsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "attachments <id>",
		Short: "List attachments for an artifact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			ctx := context.Background()
			attachments, err := svc.Proto.Store().GetAttachments(ctx, args[0])
			if err != nil {
				return err
			}
			if len(attachments) == 0 {
				fmt.Println("(no attachments)")
				return nil
			}
			for _, a := range attachments {
				fmt.Printf("%-40s  %-25s  %d bytes  base64: %s…\n",
					a.Name, a.ContentType, len(a.Data),
					base64.StdEncoding.EncodeToString(a.Data[:minInt(6, len(a.Data))]))
			}
			return nil
		},
	}
}

// inferContentType returns a MIME type for a file path based on its extension.
// Falls back to "application/octet-stream" for unknown extensions.
func inferContentType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".webp":
		return "image/webp"
	case ".pdf":
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
