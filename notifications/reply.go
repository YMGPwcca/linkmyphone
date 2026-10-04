package notifications

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"
)

//go:embed reply_helper.py
var replyHelper string

const (
	maxReplyJSONBytes       = 1 << 20
	maxReplyTextBytes       = 32 << 10
	maxReplyOutputJSONBytes = 6*maxReplyTextBytes + 128
)

type replyInput struct {
	AppName         string `json:"appName"`
	Title           string `json:"title"`
	Body            string `json:"body"`
	ActionLabel     string `json:"actionLabel"`
	ActivationToken string `json:"activationToken,omitempty"`
}

type replyOutput struct {
	Submitted bool   `json:"submitted"`
	Text      string `json:"text"`
}

// ReplyAvailable checks for a usable GTK4/Python GI runtime without opening a
// window or retaining any user-provided data.
func ReplyAvailable(ctx context.Context) bool {
	return findGTKPython(ctx) != ""
}

func findGTKPython(ctx context.Context) string {
	if ctx == nil {
		ctx = context.Background()
	}
	for _, candidate := range []string{"python3", "/usr/bin/python3"} {
		path, err := exec.LookPath(candidate)
		if err != nil {
			continue
		}
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		cmd := exec.CommandContext(probeCtx, path, "-c", "import gi; gi.require_version('Gtk', '4.0'); from gi.repository import Gtk")
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		err = cmd.Run()
		cancel()
		if err == nil {
			return path
		}
	}
	return ""
}

// PromptReply opens the real native reply window and returns only the text
// explicitly submitted by the user. Closing or pressing Cancel is a normal
// non-submission; context cancellation is returned as an error.
func PromptReply(ctx context.Context, request ReplyPrompt) (string, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateReplyPrompt(request); err != nil {
		return "", false, err
	}
	python := findGTKPython(ctx)
	if python == "" {
		if err := ctx.Err(); err != nil {
			return "", false, err
		}
		return "", false, errors.New("notifications: GTK4/Python reply runtime unavailable")
	}
	input, err := json.Marshal(replyInput{
		AppName: request.AppName, Title: request.Title, Body: request.Body,
		ActionLabel: request.ActionLabel, ActivationToken: request.ActivationToken,
	})
	if err != nil {
		return "", false, err
	}
	if len(input) > maxReplyJSONBytes {
		return "", false, errors.New("notifications: reply prompt exceeds size limit")
	}

	cmd := exec.CommandContext(ctx, python, "-c", replyHelper)
	if request.ActivationToken != "" {
		cmd.Env = append(os.Environ(), "XDG_ACTIVATION_TOKEN="+request.ActivationToken)
	}
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stderr = io.Discard
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", false, errors.New("notifications: reply prompt failed")
	}
	if err := cmd.Start(); err != nil {
		return "", false, errors.New("notifications: reply prompt failed")
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, maxReplyOutputJSONBytes+1))
	if len(output) > maxReplyOutputJSONBytes {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return "", false, errors.New("notifications: reply prompt returned oversized output")
	}
	waitErr := cmd.Wait()
	if readErr != nil || waitErr != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return "", false, contextErr
		}
		return "", false, errors.New("notifications: reply prompt failed")
	}
	var result replyOutput
	if err := json.Unmarshal(output, &result); err != nil {
		return "", false, errors.New("notifications: reply prompt returned invalid output")
	}
	if len(result.Text) > maxReplyTextBytes {
		return "", false, errors.New("notifications: reply text exceeds size limit")
	}
	if !result.Submitted {
		return "", false, nil
	}
	return result.Text, true, nil
}

func validateReplyPrompt(request ReplyPrompt) error {
	if request.AppName == "" || len(request.AppName) > maxReplyTextBytes ||
		len(request.Title) > maxReplyTextBytes || len(request.Body) > maxReplyTextBytes ||
		len(request.ActionLabel) > maxReplyTextBytes || len(request.ActivationToken) > maxReplyTextBytes {
		return errors.New("notifications: reply prompt exceeds size limit")
	}
	return nil
}
