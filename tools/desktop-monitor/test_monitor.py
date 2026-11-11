import json
from pathlib import Path
import tempfile
import threading
import time
import unittest
from unittest.mock import patch

from monitor import Client, Config, Monitor, project, today


class MonitorTests(unittest.TestCase):
    def config(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        path = Path(temp.name) / '.env'
        path.write_text('SUB2API_BASE_URL=https://example.com\nSUB2API_ADMIN_API_KEY=test-key\n', encoding='utf-8')
        return Config(path)

    def test_projection_preserves_missing_and_zero_and_excludes_secrets(self):
        result = project('stats', {'stats': {'rpm': 0, 'today_cost': 10, 'credentials': 'secret'}})
        self.assertEqual(result['rpm'], 0)
        self.assertIsNone(result['today_account_cost'])
        self.assertNotIn('secret', json.dumps(result))
        self.assertEqual(project('capacity', {'oauth_status': {'normal': 3}, 'oauth_quota': {'5h': {
            'remaining_cost': 1.23, 'estimated_cost': 50, 'estimated_accounts': 4, 'total_accounts': 9}}})['remaining_cost'], 1.23)

    def test_concurrency_uses_server_platform_aggregate_not_sum(self):
        result = project('concurrency', {'enabled': True, 'platform': {'openai': {'current_in_use': 7}},
                                        'account': {'1': {'current_in_use': 500}}})
        self.assertEqual(result['used'], 7)
        self.assertIsNone(project('concurrency', {'enabled': False})['used'])

    def test_ranking_preserves_server_order_and_cost(self):
        rows = [{'user_id': 9, 'email': '<script>x</script>', 'total_tokens': 1, 'actual_cost': .25, 'cost': 100},
                {'user_id': 8, 'email': 'second', 'total_tokens': 50, 'actual_cost': 2}]
        result = project('ranking', {'users': rows})['users']
        self.assertEqual([r['user_id'] for r in result], [9, 8])
        self.assertEqual(result[0]['actual_cost'], .25)
        self.assertNotIn('cost', result[0])

    def test_requests_are_read_only_and_redirects_are_not_followed(self):
        config = self.config()
        with patch('monitor.requests.get') as get:
            get.return_value.status_code = 200
            get.return_value.json.return_value = {'code': 0, 'data': {'users': []}}
            Client(config).fetch('ranking', '2026-11-11')
            self.assertEqual(get.call_args.kwargs['params']['timezone'], 'Asia/Shanghai')
            self.assertEqual(get.call_args.kwargs['params']['sort_by'], 'total_tokens')
            self.assertFalse(get.call_args.kwargs['allow_redirects'])
            get.return_value.status_code = 302
            with self.assertRaisesRegex(ValueError, 'HTTP 302'):
                Client(config).fetch('ranking', today())

    def test_failure_retains_old_value_but_marks_it_stale(self):
        m = Monitor(self.config())
        m.records['stats'] = {'data': {'rpm': 7}, 'updated_at': time.time(), 'day': today()}
        with patch('monitor.Client.fetch', side_effect=ValueError('读取超时')):
            m.poll()
        row = m.snapshot()['sections']['stats']
        self.assertEqual(row['data']['rpm'], 7)
        self.assertTrue(row['stale'])
        self.assertEqual(row['error'], '读取超时')

    def test_midnight_never_displays_yesterdays_totals_as_today(self):
        m = Monitor(self.config())
        for name in ('stats', 'ranking'):
            m.records[name] = {'data': {'today_tokens': 500}, 'updated_at': time.time(), 'day': '2001-01-01'}
            self.assertIsNone(m.snapshot()['sections'][name]['data'])
            self.assertTrue(m.snapshot()['sections'][name]['stale'])

    def test_config_switch_discards_in_flight_old_host(self):
        m = Monitor(self.config())
        changed = threading.Event()
        def delayed(*args):
            if not changed.is_set():
                changed.set()
                m.configure(self.config())
            return {'rpm': 99}
        with patch('monitor.Client.fetch', side_effect=delayed):
            m.poll()
        self.assertEqual(m.snapshot()['sections'], {})

    def test_hidden_ranking_is_not_polled_and_visible_ranking_is_throttled(self):
        m = Monitor(self.config())
        with patch('monitor.Client.fetch', return_value={}) as fetch:
            m.poll()
            self.assertNotIn('ranking', [c.args[0] for c in fetch.call_args_list])
            m.page = 'ranking'
            m.poll()
            m.poll()
            self.assertEqual([c.args[0] for c in fetch.call_args_list].count('ranking'), 1)

    def test_http_base_url_rejected(self):
        config = self.config()
        config.path.write_text('SUB2API_BASE_URL=http://example.com\nSUB2API_ADMIN_API_KEY=x')
        with self.assertRaises(ValueError):
            Config(config.path)


if __name__ == '__main__':
    unittest.main()
