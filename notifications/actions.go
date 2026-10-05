package notifications

import (
	"context"
	"errors"
	"strconv"
	"strings"

	wire "github.com/YMGPwcca/linkmyphone/protocol/notifications"
)

func (c *Client) Dismiss(ctx context.Context, key string) error {
	c.stateMu.Lock()
	err := c.authorize(key, 0)
	if err == nil && !c.records[key].item.IsClearable {
		err = errors.New("notifications: ongoing notification cannot be dismissed")
	}
	var guards []mutationGuard
	var revision uint64
	if err == nil {
		revision = c.records[key].revision
		guards = []mutationGuard{{key: key, revision: revision, clearable: true}}
	}
	c.stateMu.Unlock()
	if err != nil {
		return err
	}
	cv, err := requestID()
	if err != nil {
		return err
	}
	values, err := wire.DismissRequest(c.cfg.Info, cv, key)
	if err != nil {
		return err
	}
	_, err = c.request(ctx, wire.RouteNotifications, values, guards)
	if err == nil {
		c.event("phone accepted notification request", map[string]string{
			"operation": "dismiss", "outcome": "accepted", "record_ref": strconv.FormatUint(revision, 10),
		})
	}
	return err
}

// Clear clears only explicit current, clearable keys. An empty list is never
// sent: the phone interprets it as cancelAllNotifications, outside this cache.
func (c *Client) Clear(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return errors.New("notifications: clear requires explicit keys")
	}
	c.stateMu.Lock()
	var err error
	guards := make([]mutationGuard, 0, len(keys))
	for _, key := range keys {
		if err = c.authorize(key, 0); err != nil {
			break
		}
		if !c.records[key].item.IsClearable {
			err = errors.New("notifications: clear contains ongoing notification")
			break
		}
		guards = append(guards, mutationGuard{key: key, revision: c.records[key].revision, clearable: true})
	}
	c.stateMu.Unlock()
	if err != nil {
		return err
	}
	cv, err := requestID()
	if err != nil {
		return err
	}
	values, err := wire.ClearRequest(c.cfg.Info, cv, keys)
	if err != nil {
		return err
	}
	_, err = c.request(ctx, wire.RouteNotifications, values, guards)
	if err == nil {
		c.event("phone accepted notification request", map[string]string{
			"operation": "clear", "outcome": "accepted", "count": strconv.Itoa(len(keys)),
		})
	}
	return err
}

func (c *Client) Action(ctx context.Context, key string, index int32, reply *string) error {
	return c.action(ctx, key, 0, index, reply)
}
func (c *Client) action(ctx context.Context, key string, revision uint64, index int32, reply *string) error {
	c.stateMu.Lock()
	err := c.authorize(key, revision)
	if err == nil && index >= 0 {
		found := false
		for _, action := range c.records[key].item.Actions {
			if action.Index == index {
				found = true
				if action.InlineReply != (reply != nil) {
					err = errors.New("notifications: reply does not match Android action type")
				}
				break
			}
		}
		if !found {
			err = ErrStaleAction
		}
	} else if err == nil && reply != nil {
		err = ErrStaleAction
	}
	if err == nil {
		revision = c.records[key].revision
	}
	c.stateMu.Unlock()
	if err != nil {
		return err
	}
	cv, err := requestID()
	if err != nil {
		return err
	}
	values, err := wire.ActionRequest(c.cfg.Info, cv, key, index, reply)
	if err != nil {
		return err
	}
	if reply != nil && strings.TrimSpace(*reply) == "" {
		return errors.New("notifications: reply must contain text")
	}
	_, err = c.request(ctx, wire.RouteNotifications, values, []mutationGuard{{key: key, revision: revision}})
	if err == nil {
		operation := "button"
		if reply != nil {
			operation = "reply"
		} else if index < 0 {
			operation = "launch"
		}
		c.event("phone accepted notification request", map[string]string{
			"operation": operation, "outcome": "accepted", "record_ref": strconv.FormatUint(revision, 10),
		})
	}
	return err
}

// authorize runs under stateMu. The revision is the desktop-render generation,
// so a queued button or reply cannot act on an updated Android PendingIntent.
func (c *Client) authorize(key string, revision uint64) error {
	if !c.ready || !c.desktopAvailable {
		return ErrNotReady
	}
	if !c.cfg.RemoteActions {
		return ErrReceiveOnly
	}
	record := c.records[key]
	if record == nil || revision != 0 && record.revision != revision {
		return ErrStaleAction
	}
	return nil
}

func (c *Client) actionLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-c.jobs:
			if err := c.handleNative(ctx, event); err != nil && !errors.Is(err, context.Canceled) {
				c.event("desktop notification action failed", map[string]string{"reason": err.Error()})
			}
		}
	}
}
func (c *Client) handleNative(ctx context.Context, event NativeEvent) error {
	c.stateMu.Lock()
	key := c.desktopIDs[event.ID]
	record := c.records[key]
	if record == nil {
		c.stateMu.Unlock()
		return nil
	}
	if event.Kind == NativeClosed {
		delete(c.desktopIDs, event.ID)
		if record.replyCancel != nil {
			record.replyCancel()
		}
		record.desktopID = 0
		record.hidden = event.Reason == 2
		forward := event.Reason == 2 && record.item.IsClearable && c.cfg.RemoteActions && c.ready
		ref := strconv.FormatUint(record.revision, 10)
		c.stateMu.Unlock()
		if event.Reason == 2 {
			c.event("desktop notification dismissed", map[string]string{
				"record_ref": ref, "remote_request": strconv.FormatBool(forward),
			})
		}
		if forward {
			return c.Dismiss(ctx, key)
		}
		return nil
	}
	if event.Kind != NativeAction {
		c.stateMu.Unlock()
		return nil
	}
	err := c.authorize(key, record.revision)
	if err != nil {
		c.stateMu.Unlock()
		return err
	}
	if event.Action == "default" {
		revision := record.revision
		c.stateMu.Unlock()
		return c.action(ctx, key, revision, -1, nil)
	}
	parts := strings.Split(event.Action, ":")
	if len(parts) != 3 || parts[0] != "action" {
		c.stateMu.Unlock()
		return ErrStaleAction
	}
	revision, parseErr := strconv.ParseUint(parts[1], 10, 64)
	index, indexErr := strconv.ParseInt(parts[2], 10, 32)
	if parseErr != nil || indexErr != nil || revision != record.revision {
		c.stateMu.Unlock()
		return ErrStaleAction
	}
	var selected *wire.Action
	for i := range record.item.Actions {
		if record.item.Actions[i].Index == int32(index) {
			selected = &record.item.Actions[i]
			break
		}
	}
	if selected == nil {
		c.stateMu.Unlock()
		return ErrStaleAction
	}
	if !selected.InlineReply {
		c.stateMu.Unlock()
		return c.action(ctx, key, revision, int32(index), nil)
	}
	if !c.cfg.ReplyEnabled {
		c.stateMu.Unlock()
		return errors.New("notifications: native reply runtime unavailable")
	}
	if record.replyCancel != nil {
		c.stateMu.Unlock()
		return errors.New("notifications: reply already open")
	}
	select {
	case c.promptSlots <- struct{}{}:
	default:
		c.stateMu.Unlock()
		return errors.New("notifications: too many open reply windows")
	}
	replyCtx, cancel := context.WithCancel(ctx)
	record.replyCancel = cancel
	prompt := ReplyPrompt{AppName: record.item.AppName, Title: record.item.Title, Body: record.item.Text, ActionLabel: selected.Name, ActivationToken: record.activationToken}
	if prompt.AppName == "" {
		prompt.AppName = record.item.PackageName
	}
	if prompt.AppName == "" {
		prompt.AppName = "Phone notification"
	}
	c.stateMu.Unlock()
	c.prompts.Add(1)
	go func() {
		defer c.prompts.Done()
		defer func() { <-c.promptSlots }()
		text, submitted, promptErr := c.cfg.Prompt(replyCtx, prompt)
		cancel()
		c.stateMu.Lock()
		if current := c.records[key]; current != nil && current.revision == revision {
			current.replyCancel = nil
		}
		c.stateMu.Unlock()
		if promptErr == nil && submitted {
			promptErr = c.action(ctx, key, revision, int32(index), &text)
		}
		if promptErr != nil && !errors.Is(promptErr, context.Canceled) {
			c.event("notification reply failed", map[string]string{"reason": promptErr.Error()})
		}
	}()
	return nil
}
