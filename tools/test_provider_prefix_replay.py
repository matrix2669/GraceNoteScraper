"""Protected replay counts physical regressions, not just raw-name classes."""
import unittest

from replay_provider_prefixes import record_denied_pairs


class DeniedPairTests(unittest.TestCase):
    def test_same_raw_name_new_account_is_new_denial(self):
        detail = dict(channelId='cnn', streamKey='3:10', rawName='Provider: CNN')
        old = {('cnn', '3:10')}
        new = old | {('cnn', '4:20'), ('other', '4:20')}
        denied = {('cnn', '3:10'), ('cnn', '4:20')}
        report = dict(newlyMatchingDenials=[], previouslyMatchingDenials=[])
        record_denied_pairs(report, detail, old, new, denied)
        self.assertEqual([x['streamKey'] for x in report['newlyMatchingDenials']], ['4:20'])
        self.assertEqual([x['streamKey'] for x in report['previouslyMatchingDenials']], ['3:10'])
        self.assertEqual(report['newlyMatchingDenials'][0]['decisionStreamKey'], '3:10')
        self.assertEqual(report['newlyMatchingDenials'][0]['rawName'], 'Provider: CNN')

    def test_removed_prior_denial_remains_baseline_diagnostic(self):
        detail = dict(channelId='cnn', streamKey='3:10', rawName='Provider: CNN')
        denied = {('cnn', '3:10')}
        report = dict(newlyMatchingDenials=[], previouslyMatchingDenials=[])
        record_denied_pairs(report, detail, denied, set(), denied)
        self.assertEqual(report['newlyMatchingDenials'], [])
        self.assertEqual(len(report['previouslyMatchingDenials']), 1)


if __name__ == '__main__':
    unittest.main()
