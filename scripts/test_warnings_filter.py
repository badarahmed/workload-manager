import io
import unittest
from warnings_filter import WarningFilter, TITLE


class WarningFilterTests(unittest.TestCase):
    def filtered(self, value, size):
        output = io.StringIO()
        parser = WarningFilter(output)
        for start in range(0, len(value), size):
            parser.feed(value[start:start + size])
        parser.finish()
        return output.getvalue()

    def test_plain_and_colored_framed_diagnostics(self):
        plain = TITLE + '\n\nDetails\nreleases.\n'
        frame = '\x1b[33m╷\x1b[0m\n│ ' + TITLE + '\n│ Details\n╵\n'
        for warning in (plain, frame):
            for size in (1, 3, 4096):
                value = 'Plan starts\n' + warning + 'Plan: 1 to change\n'
                self.assertEqual(self.filtered(value, size), 'Plan starts\nPlan: 1 to change\n')

    def test_unrelated_warnings_errors_json_and_truncated_blocks_are_retained(self):
        for value in ('╷\n│ Error: failed\n╵\n', 'Warning: other warning\n', TITLE+'\ntruncated', '{"diagnostic":"'+TITLE+'"}\n'):
            self.assertEqual(self.filtered(value, 1), value)

    def test_prompt_without_newline_is_forwarded_immediately(self):
        output = io.StringIO()
        parser = WarningFilter(output)
        parser.feed('Enter a value: ')
        self.assertEqual(output.getvalue(), 'Enter a value: ')


if __name__ == '__main__':
    unittest.main()
