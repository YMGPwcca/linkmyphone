package clipboard

import (
	"context"
	"crypto/sha256"
	"time"

	clipclient "github.com/YMGPwcca/linkmyphone/clipboard"
	proto "github.com/YMGPwcca/linkmyphone/protocol/clipboard"
)

func contentHashes(content clipclient.Content) ([32]byte, [32]byte) {
	if content.Type == proto.ItemTextPlain {
		return sha256.Sum256([]byte(content.Text)), clipboardTrackingHash(content.Text)
	}
	h := sha256.New()
	h.Write([]byte{byte(content.Type)})
	h.Write([]byte(content.Text))
	h.Write(content.Image)
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum, sum
}

func (l *trackedLocalClipboard) ReadContent(ctx context.Context) (clipclient.Content, error) {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	if rich, ok := l.base.(clipclient.ContentLocal); ok {
		return rich.ReadContent(ctx)
	}
	text, err := l.base.ReadText(ctx)
	return clipclient.TextContent(text), err
}

func (l *trackedLocalClipboard) WriteContent(ctx context.Context, content clipclient.Content) error {
	if content.Type == proto.ItemTextPlain {
		return l.WriteText(ctx, content.Text)
	}
	rich, ok := l.base.(clipclient.ContentLocal)
	if !ok {
		return clipclient.ErrUnsupportedContent
	}
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	exact, tracking := contentHashes(content)
	now := time.Now()
	l.mu.Lock()
	l.pruneSuppressedLocked(now)
	l.applying = true
	if l.initialized {
		l.suppress[l.lastTrackingHash] = now.Add(remoteClipboardSettleWindow)
	}
	l.suppress[tracking] = now.Add(remoteClipboardSettleWindow)
	l.mu.Unlock()
	err := rich.WriteContent(ctx, content)
	l.mu.Lock()
	l.applying = false
	if err == nil {
		l.lastExactHash = exact
		l.lastTrackingHash = tracking
		l.initialized = true
	} else {
		delete(l.suppress, tracking)
	}
	l.mu.Unlock()
	return err
}

func (l *trackedLocalClipboard) MarkContentIfChanged(content clipclient.Content) bool {
	if content.Type == proto.ItemTextPlain {
		return l.MarkIfChanged(content.Text)
	}
	exact, tracking := contentHashes(content)
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneSuppressedLocked(now)
	if l.applying {
		return false
	}
	if until, ok := l.suppress[tracking]; ok && now.Before(until) {
		l.lastExactHash = exact
		l.lastTrackingHash = tracking
		l.initialized = true
		delete(l.suppress, tracking)
		return false
	}
	if l.initialized && l.lastExactHash == exact {
		return false
	}
	l.lastExactHash = exact
	l.lastTrackingHash = tracking
	l.initialized = true
	return true
}

func (i *instance) observeLocalContent(content clipclient.Content) {
	if !i.local.MarkContentIfChanged(content) {
		return
	}
	generation := i.client.ReserveLocalGeneration()
	copy := content.Clone()
	queueLatestPublish(i.publishQueue, publishJob{generation: generation, text: content.Text, content: &copy})
}
