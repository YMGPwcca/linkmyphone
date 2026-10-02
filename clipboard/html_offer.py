"""Own an HTML selection with a plain-text fallback until replaced."""
import json
import os
import sys
from html.parser import HTMLParser

import gi
gi.require_version("Gtk", "4.0")
gi.require_version("Gdk", "4.0")
from gi.repository import Gdk, GLib, Gtk


class PlainText(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.parts = []
        self.hidden = 0

    def handle_starttag(self, tag, attrs):
        if tag in ("script", "style"):
            self.hidden += 1
        if not self.hidden and tag in ("br", "p", "div", "li", "tr"):
            if self.parts and not self.parts[-1].endswith("\n"):
                self.parts.append("\n")

    def handle_endtag(self, tag):
        if tag in ("script", "style"):
            self.hidden = max(0, self.hidden - 1)
        if not self.hidden and tag in ("p", "div", "li", "tr"):
            if self.parts and not self.parts[-1].endswith("\n"):
                self.parts.append("\n")

    def handle_data(self, data):
        if not self.hidden:
            self.parts.append(data)


html = json.loads(sys.stdin.read(1 << 20))
if not isinstance(html, str):
    raise ValueError("HTML string required")
parser = PlainText()
parser.feed(html)
plain = "".join(parser.parts).rstrip("\n")
Gtk.init()
display = Gdk.Display.get_default()
if display is None:
    raise RuntimeError("no graphical display")
clipboard = display.get_clipboard()
providers = [
    Gdk.ContentProvider.new_for_bytes("text/html", GLib.Bytes.new(html.encode("utf-8"))),
    Gdk.ContentProvider.new_for_value(plain),
]
provider = Gdk.ContentProvider.new_union(providers)
loop = GLib.MainLoop()
if not clipboard.set_content(provider):
    raise RuntimeError("clipboard offer rejected")


def check_owner():
    if clipboard.get_content() != provider:
        loop.quit()
        return False
    return True


GLib.timeout_add(250, check_owner)
display.flush()
sys.stdout.write("READY\n")
sys.stdout.flush()
os.close(1)
loop.run()
