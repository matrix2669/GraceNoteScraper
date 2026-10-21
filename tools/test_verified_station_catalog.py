"""Offline provenance/admission regressions; synthetic data is never bundled."""
import tempfile
import unittest
from pathlib import Path
from generate_verified_station_catalog import generate, pinned_text

EMPTY_PRISM = 'const BASE_CHANNEL_DEFINITIONS = {}'
class VerifiedCatalogTests(unittest.TestCase):
    def test_unpinned_input_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'input'
            path.write_text('arbitrary data cannot claim a pinned revision')
            with self.assertRaisesRegex(ValueError, 'pinned Git blob'):
                pinned_text('prismcast', path)

    def test_single_local_user_of_fallback_cannot_launder_callsign(self):
        stations = 'channel_id,call_sign,original_name,clean_name\nPBS_WAAA,WAAA,PBS Alpha,PBS Alpha\n'
        mapping = 'channel_id,gracenote_id,gracenote_id_natural\nPBS_WAAA_kids_main,100,101\n'
        rows = generate(EMPTY_PRISM, stations, mapping)['channels']
        fallback = [r for r in rows if r['stationId']=='100']
        local = [r for r in rows if r['stationId']=='101']
        self.assertEqual(fallback[0]['aliases'], ['PBS Kids'])
        self.assertIn('WAAA PBS Kids',local[0]['aliases'])

    def test_nested_identity_never_promoted_and_positions_never_aliases(self):
        prism = '''const BASE_CHANNEL_DEFINITIONS = {
  alpha: {
    name: "Alpha",
    stationId: "101",
    services: {x: {stationId: "999", channelSelector: "Alpha HD 635"}},
  },
  no_identity: {
    name: "No Identity",
    services: {x: {stationId: "102", channelSelector: "Not Portable"}},
  },
}'''
        rows = generate(prism,'channel_id\n','channel_id\n')['channels']
        self.assertEqual(len(rows),1)
        self.assertEqual(rows[0]['stationId'],'101')
        self.assertEqual(rows[0]['aliases'],['Alpha'])

    def test_quarantine_does_not_turn_shared_alias_into_unique_proof(self):
        prism='const BASE_CHANNEL_DEFINITIONS = {\n'
        for key,name,sid in [('one','One','1'),('two','Two','2'),('conflict','Another','2')]:
            prism += f'  {key}: {{\n    name: "{name}",\n    stationId: "{sid}",\n    services: {{x: {{channelSelector: "Shared"}}}},\n  }},\n'
        rows=generate(prism+'}','channel_id\n','channel_id\n')['channels']
        self.assertEqual(len(rows),1)
        self.assertEqual(rows[0]['aliases'],['One'])

if __name__ == '__main__': unittest.main()
