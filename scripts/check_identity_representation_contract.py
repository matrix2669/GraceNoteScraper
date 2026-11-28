"""Generated builder JSON against unchanged consumer; no network or live state."""
from pathlib import Path
import json, logging, os, subprocess, sys, tempfile
root=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(root/'lineuparr_matcher'))
from fuzzy_matcher import FuzzyMatcher
logging.disable(logging.CRITICAL)
with tempfile.TemporaryDirectory() as directory:
    path=Path(directory)/'fixture.json'
    subprocess.run(['go','test','./lineuparr','-run','^TestIdentityRepresentationJSONFixture$','-count=1'],cwd=root,env=dict(os.environ,IDENTITY_EXPORT_FIXTURE=str(path)),check=True)
    entries={e['name']:e for rows in json.loads(path.read_text())['categories'].values() for e in rows}
    cases={
      'GRIOCH':(['TV: THEGRIO ᴿᴬᵂ'],[]),
      'WZMEDT2':(['US: MeTV Plus (WZME) HD (SP)','US| MeTV PLUS'],['US| MeTV']),
      'RETROHD':(['US: RETROPLEX HD','US| RETROPLEX HD'],['US: RETRO TV','US| RETRO TV']),
    }
    for threshold in (70,95):
      matcher=FuzzyMatcher(match_threshold=threshold)
      for target,(positive,negative) in cases.items():
        names=positive+negative;matcher.precompute_normalizations(names)
        hits=matcher.match_all_streams(target,names,{target:entries[target]['aliases']},None,lineup_country='US')
        assert {name for name,_,_ in hits}==set(positive),(threshold,target,hits)
    print('Actual generated JSON: admitted TheGrio case, MeTV Plus distinct service and typed compound RetroPlex positive/negative controls passed at70/95; consumer unchanged.')

with tempfile.TemporaryDirectory() as directory:
    path=Path(directory)/'subchannels.json'
    subprocess.run(['go','test','./lineuparr','-run','^TestVerifiedSubchannelJSONFixture$','-count=1'],cwd=root,env=dict(os.environ,SUBCHANNEL_EXPORT_FIXTURE=str(path)),check=True)
    entries={e['name']:e for rows in json.loads(path.read_text())['categories'].values() for e in rows}
    for threshold in (70,95):
        matcher=FuzzyMatcher(match_threshold=threshold)
        for target,entry in entries.items():
            assert not any('pbs subchannel' in a.casefold() for a in entry.get('aliases',[]))
            hits=matcher.match_all_streams(target,['US: WETA PBS subchannel'],{target:entry.get('aliases',[])},None,lineup_country='US')
            assert not hits,(threshold,target,hits)
    print('Distinct PBS subchannels: generic synthesized alias absent through registry admission, JSON and unchanged consumer at70/95.')
