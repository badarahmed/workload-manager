"""Suppress only the known dev-override diagnostic; pass unknown formats through."""
import codecs
import os
import re

ANSI = re.compile(r'\x1b\[[0-?]*[ -/]*[@-~]')
TITLE = 'Warning: Provider development overrides are in effect'


class WarningFilter:
    def __init__(self, output):
        self.output = output
        self.line = ''
        self.block = ''
        self.framed = False
        self.matched = False
        self.passthrough = False

    def emit(self, text):
        self.output.write(text)
        self.output.flush()

    def complete_line(self, line):
        plain = ANSI.sub('', line).strip()
        if not self.block:
            if plain == '╷':
                self.block, self.framed = line, True
            elif plain == TITLE:
                self.block, self.matched, self.framed = line, True, False
            else:
                self.emit(line)
            return
        self.block += line
        body = plain.lstrip('│ ').strip()
        if not self.matched:
            if body == TITLE:
                self.matched = True
            else:
                self.emit(self.block)
                self.block = ''
        elif (self.framed and plain == '╵') or (not self.framed and body == 'releases.'):
            self.block = ''
            self.matched = False
        elif len(self.block) > 65536 or body.startswith(('Error:', 'Warning:')):
            # Changed/truncated diagnostic format: retain rather than hide output.
            self.emit(self.block)
            self.block = ''
            self.matched = False

    def feed(self, text):
        for part in text.splitlines(keepends=True):
            self.line += part
            if self.line.endswith('\n'):
                if self.passthrough:
                    self.emit(self.line)
                else:
                    self.complete_line(self.line)
                self.line = ''
                self.passthrough = False
            elif not self.block:
                plain = ANSI.sub('', self.line).strip()
                candidate = TITLE.startswith(plain) or '╷'.startswith(plain) or '\x1b' in plain
                if self.passthrough or not candidate:
                    # Forward prompts without waiting for a newline.
                    self.emit(self.line)
                    self.line = ''
                    self.passthrough = True

    def finish(self):
        self.emit(self.block + self.line)
        self.block = self.line = ''


def filter_stream(stream, output):
    parser = WarningFilter(output)
    decoder = codecs.getincrementaldecoder('utf-8')('replace')
    try:
        while True:
            chunk = os.read(stream.fileno(), 4096)
            if not chunk:
                break
            parser.feed(decoder.decode(chunk))
        parser.feed(decoder.decode(b'', final=True))
        parser.finish()
    finally:
        stream.close()
