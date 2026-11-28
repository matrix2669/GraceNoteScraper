"""Display attribution is exact lookup evidence, never another scoring pass."""
import io
import json
import logging
import sys
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from unittest.mock import patch
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'lineuparr_matcher'))
import runner
from fuzzy_matcher import FuzzyMatcher

class AttributionTests(unittest.TestCase):
    def test_lookup_collisions_and_unavailable(self):
        m = FuzzyMatcher(logger=logging.getLogger('test'))
        aliases = ['HBO 2', 'HBO2', 'HBO 2 HD']
        lookup = runner.exact_alias_lookup(m, aliases)
        for raw in ['HBO2', 'US: HBO 2 HD', 'US| HBO2 FHD']:
            self.assertEqual(runner.exact_alias_evidence(m, lookup, raw, 'alias'), 'HBO 2 HD')
        self.assertEqual(runner.exact_alias_evidence(m, lookup, 'HBO 2', 'callsign'), '')
        self.assertEqual(runner.exact_alias_evidence(m, lookup, 'HBO 2', 'exact'), '')
        self.assertEqual(runner.exact_alias_evidence(m, lookup, 'HBO Other', 'alias'), '')

    def test_one_call_per_channel_unchanged_results_and_country_filter(self):
        channels = [dict(id='h',name='HBOHTHD',number='902',aliases=['HBO 2','HBO2']),
                    dict(id='f',name='Different Service',number='123',aliases=['National Geographic Documentary Television Channel']),
                    dict(id='b',name='BBC One',number='1',aliases=['BBC 1'])]
        names = ['US: HBO2 HD','HBO 2','National Geographic Documentary Television Channel Live','UK| BBC 1 HD']
        request = dict(country='US',channels=channels,streams=[dict(key=str(i),name=n) for i,n in enumerate(names)])
        m = FuzzyMatcher(match_threshold=70,logger=logging.getLogger('test'))
        m.precompute_normalizations(names)
        expected = []
        for c in channels:
            for name,score,kind in m.match_all_streams(c['name'],names,{c['name']:c['aliases']},runner.channel_number(c['number']),lineup_country='US'):
                expected.append(dict(channelId=c['id'],streamKey=str(names.index(name)),score=score,reason='Lineuparr '+kind))
        calls=[]
        original=FuzzyMatcher.match_all_streams
        def spy(self,*args,**kwargs):
            calls.append((args,kwargs)); return original(self,*args,**kwargs)
        output=io.StringIO()
        with patch.object(sys,'stdin',io.StringIO(json.dumps(request))), patch.object(FuzzyMatcher,'match_all_streams',spy), redirect_stdout(output):
            runner.main()
        actual=json.loads(output.getvalue())['matches']
        self.assertEqual(len(calls),len(channels))
        self.assertEqual([{k:v for k,v in row.items() if k!='exactAlias'} for row in actual],expected)
        self.assertTrue(any(row['exactAlias'] for row in actual if row['channelId']=='h'))
        self.assertTrue(any(row['channelId']=='f' and row['reason']=='Lineuparr alias' and not row['exactAlias'] for row in actual))
        self.assertFalse(any(row['channelId']=='b' for row in actual))

if __name__ == '__main__': unittest.main()
