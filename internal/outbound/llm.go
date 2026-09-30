package outbound

// Asking the language model: the optional weekly summary and reading
// invoices from mail. Services call these, never the driver.

import (
	"context"

	"andon/internal/drivers/llm"
)

// LLMFile is an attachment the model reads (PDF, image).
type LLMFile = llm.File

// LLMReadable reports whether the model reads files of media type.
func LLMReadable(media string) bool { return llm.Readable(media) }

// Ask sends one prompt and returns the answer.
func Ask(ctx context.Context, apiKey, system, prompt string) (string, error) {
	return llm.Complete(ctx, apiKey, system, prompt)
}

// AskFiles sends a prompt with attachments.
func AskFiles(ctx context.Context, apiKey, system, prompt string, files []LLMFile) (string, error) {
	return llm.CompleteFiles(ctx, apiKey, system, prompt, files)
}
