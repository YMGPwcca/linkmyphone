import json
import sys

import gi
gi.require_version("Gtk", "4.0")
from gi.repository import Gdk, Gio, GLib, Gtk, Pango

MAX_INPUT = 1 << 20


def read_request():
    raw = sys.stdin.read(MAX_INPUT + 1)
    if len(raw) > MAX_INPUT:
        raise ValueError("request too large")
    value = json.loads(raw)
    if not isinstance(value, dict):
        raise ValueError("request must be an object")
    fields = {}
    for key in ("appName", "title", "body", "actionLabel", "activationToken"):
        item = value.get(key, "")
        if not isinstance(item, str):
            raise ValueError("request field must be text")
        fields[key] = item
    if not fields["appName"]:
        raise ValueError("app name is required")
    return fields


class ReplyWindow(Gtk.Window):
    def __init__(self, request, finish):
        super().__init__(title=request["appName"] or "Reply")
        self._finish = finish
        self._done = False
        self.set_modal(True)
        self.set_default_size(480, 360)

        root = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=12)
        root.set_margin_top(20)
        root.set_margin_bottom(20)
        root.set_margin_start(20)
        root.set_margin_end(20)
        self.set_child(root)

        title = Gtk.Label(label=request["title"])
        title.set_halign(Gtk.Align.START)
        title.set_wrap(True)
        title.set_wrap_mode(Pango.WrapMode.WORD_CHAR)
        title.set_selectable(True)
        title.set_lines(2)
        title.set_ellipsize(Pango.EllipsizeMode.END)
        root.append(title)

        body = Gtk.Label(label=request["body"])
        body.set_halign(Gtk.Align.START)
        body.set_valign(Gtk.Align.START)
        body.set_wrap(True)
        body.set_wrap_mode(Pango.WrapMode.WORD_CHAR)
        body.set_selectable(True)
        message_scroll = Gtk.ScrolledWindow()
        message_scroll.set_max_content_height(120)
        message_scroll.set_propagate_natural_height(True)
        message_scroll.set_child(body)
        root.append(message_scroll)

        editor_label = Gtk.Label(label="_Reply", use_underline=True)
        editor_label.set_halign(Gtk.Align.START)
        root.append(editor_label)

        scroll = Gtk.ScrolledWindow()
        scroll.set_vexpand(True)
        self.editor = Gtk.TextView()
        self.editor.set_wrap_mode(Gtk.WrapMode.WORD_CHAR)
        self.editor.set_accepts_tab(False)
        self.editor.set_vexpand(True)
        scroll.set_child(self.editor)
        editor_label.set_mnemonic_widget(self.editor)
        root.append(scroll)

        self.error = Gtk.Label()
        self.error.set_halign(Gtk.Align.START)
        self.error.set_wrap(True)
        self.error.set_wrap_mode(Pango.WrapMode.WORD_CHAR)
        self.error.set_visible(False)
        root.append(self.error)

        buttons = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=8)
        buttons.set_halign(Gtk.Align.END)
        cancel = Gtk.Button(label="Cancel")
        cancel.connect("clicked", lambda _button: self.cancel())
        send = Gtk.Button(label=request["actionLabel"] or "Send")
        send.connect("clicked", lambda _button: self.submit())
        buttons.append(cancel)
        buttons.append(send)
        root.append(buttons)

        keys = Gtk.EventControllerKey()
        keys.connect("key-pressed", self._key_pressed)
        self.add_controller(keys)
        self.connect("close-request", self._closed)

    def present_editor(self):
        self.present()
        self.editor.grab_focus()

    def _key_pressed(self, _controller, keyval, _keycode, state):
        if keyval == Gdk.KEY_Escape:
            self.cancel()
            return True
        if keyval in (Gdk.KEY_Return, Gdk.KEY_KP_Enter) and state & Gdk.ModifierType.CONTROL_MASK:
            self.submit()
            return True
        return False

    def _closed(self, _window):
        self.cancel()
        return True

    def cancel(self):
        if not self._done:
            self._done = True
            self._finish(False, "")
            self.destroy()

    def submit(self):
        buffer = self.editor.get_buffer()
        text = buffer.get_text(buffer.get_start_iter(), buffer.get_end_iter(), True)
        if not text.strip():
            self.error.set_text("Enter a reply before sending.")
            self.error.set_visible(True)
            self.editor.grab_focus()
            return
        if len(text.encode("utf-8")) > 32768:
            self.error.set_text("Reply is too long (maximum 32 KiB).")
            self.error.set_visible(True)
            self.editor.grab_focus()
            return
        self._done = True
        self._finish(True, text)
        self.destroy()


def run(request):
    result = {"submitted": False, "text": ""}
    app = Gtk.Application(application_id="com.linkmyphone.Reply", flags=Gio.ApplicationFlags.NON_UNIQUE)

    def finish(submitted, text):
        result.update(submitted=submitted, text=text)
        app.quit()

    def activate(_app):
        window = ReplyWindow(request, finish)
        window.set_application(app)
        GLib.idle_add(window.present_editor)

    app.connect("activate", activate)
    app.run([])
    sys.stdout.write(json.dumps(result, ensure_ascii=False, separators=(",", ":")))
    sys.stdout.flush()


request = read_request()
run(request)
