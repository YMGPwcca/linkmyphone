package notifications

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	wire "github.com/YMGPwcca/linkmyphone/protocol/notifications"
)

const (
	maxStateEntries = 4096
	maxStateBytes   = 32 << 20
)

type record struct {
	item            *wire.Item
	desktopID       uint32
	revision        uint64
	weight          int
	hidden          bool
	dirty           bool
	activationToken string
	replyCancel     context.CancelFunc
}

func (c *Client) reconcileState() ([]string, []int64) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	keys := make([]string, 0, len(c.records))
	times := make([]int64, 0, len(c.records))
	for key, record := range c.records {
		keys = append(keys, key)
		times = append(times, record.item.PostTime)
	}
	return keys, times
}

func (c *Client) applyBatch(ctx context.Context, batch wire.Batch) error {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	removedCount := 0
	var removedRef uint64
	for _, operation := range batch.Operations {
		switch operation.Type {
		case wire.OperationNew, wire.OperationExisting:
			if err := c.upsert(ctx, operation.Item, operation.Type == wire.OperationExisting); err != nil {
				return err
			}
		case wire.OperationRemove:
			if record := c.records[operation.Key]; record != nil {
				removedCount++
				removedRef = record.revision
			}
			if err := c.remove(ctx, operation.Key); err != nil {
				return err
			}
		case wire.OperationSessionChange:
			// Android keys contain the notification-listener session identifier.
			// Windows removes entries whose keys do not contain the announced session.
			for key, record := range c.records {
				if !strings.Contains(key, operation.Key) {
					if err := c.remove(ctx, key); err != nil {
						return err
					}
					removedCount++
					removedRef = record.revision
				}
			}
		}
	}
	fields := map[string]string{"operations": strconv.Itoa(len(batch.Operations)), "items": strconv.Itoa(len(c.records))}
	if removedCount > 0 {
		fields["removed"] = strconv.Itoa(removedCount)
		if removedCount == 1 {
			fields["removed_record_ref"] = strconv.FormatUint(removedRef, 10)
		}
	}
	c.event("notification state synchronized", fields)
	return nil
}

func (c *Client) upsert(ctx context.Context, item *wire.Item, existing bool) error {
	old := c.records[item.Key]
	if old != nil && sameItem(old.item, item) {
		if old.dirty {
			return c.show(ctx, old, existing)
		}
		return nil
	}
	weight := itemWeight(item)
	if weight > maxStateBytes {
		return errors.New("notifications: item exceeds state budget")
	}
	replacedWeight := 0
	if old != nil {
		replacedWeight = old.weight
		if old.replyCancel != nil {
			old.replyCancel()
			old.replyCancel = nil
		}
	}
	for len(c.records) >= maxStateEntries && old == nil || c.stateBytes-replacedWeight+weight > maxStateBytes {
		oldest := ""
		for key, candidate := range c.records {
			if key != item.Key && (oldest == "" || candidate.item.PostTime < c.records[oldest].item.PostTime) {
				oldest = key
			}
		}
		if oldest == "" {
			return errors.New("notifications: state budget exhausted")
		}
		if err := c.remove(ctx, oldest); err != nil {
			return err
		}
		c.event("notification state budget evicted oldest item", nil)
	}
	c.revision++
	current := &record{item: item, revision: c.revision, weight: weight}
	if old != nil {
		current.desktopID = old.desktopID
		current.hidden = old.hidden && old.item.PostTime == item.PostTime
		current.activationToken = old.activationToken
	}
	c.records[item.Key] = current
	c.stateBytes += weight - replacedWeight
	if existing && !c.cfg.ShowExisting && current.desktopID == 0 || current.hidden {
		return nil
	}
	current.dirty = true
	return c.show(ctx, current, existing)
}

func (c *Client) show(ctx context.Context, record *record, silent bool) error {
	if !c.desktopAvailable {
		record.dirty = true
		return nil
	}
	item := record.item
	body := item.BigText
	if body == "" {
		body = item.Text
	}
	if len(item.TextLines) != 0 {
		body = strings.Join(item.TextLines, "\n")
	}
	if item.SubText != "" {
		if body != "" {
			body += "\n"
		}
		body += item.SubText
	}
	summary := item.Title
	if item.ConversationTitle != "" && item.ConversationTitle != summary {
		if summary != "" {
			summary = item.ConversationTitle + " — " + summary
		} else {
			summary = item.ConversationTitle
		}
	}
	appName := item.AppName
	if appName == "" {
		appName = item.PackageName
	}
	if appName == "" {
		appName = "Phone notification"
	}
	notification := DesktopNotification{ReplaceID: record.desktopID, AppName: appName, Summary: summary, Body: body, Silent: silent, Resident: len(item.Actions) > 0 || item.IsOngoing}
	if c.cfg.RemoteActions && c.native.SupportsActions() {
		notification.Resident = true
		notification.Actions = append(notification.Actions, DesktopAction{ID: "default", Label: "Open on phone"})
		for _, action := range item.Actions {
			if action.Index < 0 || action.Name == "" || action.InlineReply && !c.cfg.ReplyEnabled {
				continue
			}
			notification.Actions = append(notification.Actions, DesktopAction{ID: fmt.Sprintf("action:%d:%d", record.revision, action.Index), Label: action.Name})
		}
	}
	encoded := item.LargeIcon
	if encoded == "" {
		encoded = item.SmallIcon
	}
	if encoded != "" {
		if base64.StdEncoding.DecodedLen(len(encoded)) > maxIconBytes {
			return errors.New("notifications: icon exceeds desktop budget")
		}
		icon, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return errors.New("notifications: invalid icon encoding")
		}
		notification.Icon = icon
	}
	notifyCtx, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
	id, err := c.native.Notify(notifyCtx, notification)
	cancel()
	if err != nil {
		return fmt.Errorf("notifications: render desktop item: %w", err)
	}
	if id == 0 {
		return errors.New("notifications: desktop returned zero notification ID")
	}
	delete(c.desktopIDs, record.desktopID)
	record.desktopID = id
	c.desktopIDs[id] = item.Key
	record.dirty = false
	return nil
}

func (c *Client) remove(ctx context.Context, key string) error {
	record := c.records[key]
	if record == nil {
		return nil
	}
	if record.desktopID != 0 {
		closeCtx, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
		err := c.native.CloseNotification(closeCtx, record.desktopID)
		cancel()
		if err != nil {
			return err
		}
	}
	delete(c.records, key)
	delete(c.desktopIDs, record.desktopID)
	c.stateBytes -= record.weight
	if record.replyCancel != nil {
		record.replyCancel()
	}
	return nil
}

func (c *Client) resetDesktop(ctx context.Context, available bool) error {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c.desktopAvailable = available
	clear(c.desktopIDs)
	for _, record := range c.records {
		wasVisible := record.desktopID != 0
		c.revision++
		record.revision = c.revision
		record.dirty = record.dirty || wasVisible
		record.desktopID = 0
		record.activationToken = ""
		if record.replyCancel != nil {
			record.replyCancel()
			record.replyCancel = nil
		}
		if available && record.dirty && !record.hidden {
			if err := c.show(ctx, record, true); err != nil {
				return err
			}
		}
	}
	c.event("desktop notification service reset", map[string]string{"available": strconv.FormatBool(available)})
	return nil
}

func (c *Client) closeLocal() {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c.ready = false
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	clear(c.desktopIDs)
	for _, record := range c.records {
		if record.replyCancel != nil {
			record.replyCancel()
		}
		if record.desktopID != 0 {
			_ = c.native.CloseNotification(ctx, record.desktopID)
		}
	}
	clear(c.records)
	c.stateBytes = 0
}

func sameItem(a, b *wire.Item) bool {
	return a.ID == b.ID && a.Key == b.Key && a.PackageName == b.PackageName && a.AppName == b.AppName && a.Title == b.Title && a.Text == b.Text && a.BigText == b.BigText && a.SubText == b.SubText && a.ConversationTitle == b.ConversationTitle && a.PostTime == b.PostTime && a.IsClearable == b.IsClearable && a.IsOngoing == b.IsOngoing && a.LargeIcon == b.LargeIcon && a.SmallIcon == b.SmallIcon && slices.Equal(a.TextLines, b.TextLines) && slices.Equal(a.Actions, b.Actions)
}
func itemWeight(item *wire.Item) int {
	size := 128 + len(item.Key) + len(item.PackageName) + len(item.AppName) + len(item.Title) + len(item.Text) + len(item.BigText) + len(item.SubText) + len(item.ConversationTitle) + len(item.LargeIcon) + len(item.SmallIcon)
	for _, text := range item.TextLines {
		size += len(text)
	}
	for _, action := range item.Actions {
		size += 32 + len(action.Name)
	}
	return size
}
