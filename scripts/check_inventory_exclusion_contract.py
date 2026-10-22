"""Actual Go JSON plus the unchanged consumer; no live fetch or retained writes."""
from pathlib import Path
import json,logging,os,subprocess,sys,tempfile
root=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(root/'lineuparr_matcher'))
from fuzzy_matcher import FuzzyMatcher
logging.disable(logging.CRITICAL)
with tempfile.TemporaryDirectory() as folder:
 path=Path(folder)/'fixture.json'
 subprocess.run(['go','test','.','-run','^TestInventoryFeedJSONConsumerFixture$','-count=1'],cwd=root,env=dict(os.environ,INVENTORY_EXPORT_FIXTURE=str(path),LINEUPARR_MATCHER_PYTHON=sys.executable),check=True)
 data=json.loads(path.read_text());names=[s['name'] for s in data['streams']]
 before={e['name']:e for rows in data['before']['categories'].values() for e in rows};after={e['name']:e for rows in data['after']['categories'].values() for e in rows}
 aliases={k:v.get('aliases',[]) for k,v in after.items()}
 for threshold in (70,95):
  m=FuzzyMatcher(match_threshold=threshold);m.precompute_normalizations(names)
  def match(entry):return m.match_all_streams(entry['name'],names,aliases,lineup_country='US',excluded_aliases=entry.get('excluded_aliases'))
  for code in ('TRAVHD','FXHD'):
   old=match(before[code]);new=match(after[code])
   assert old and all('west' in x[0].lower() for x in old),(threshold,code,old)
   assert not new,(threshold,code,new)
  for code in ('HBOHDP','PASHOHW'):
   found=match(after[code]);assert found and all('west' in x[0].lower() for x in found),(threshold,code,found)
  assert any(x[0]=='US: Cinemax EAST HD' for x in match(after['MAXHDP'])), 'explicit confirmation lost'
  literal=next(e for rows in data['literal']['categories'].values() for e in rows)
  exact=r'US: Example * \\ WEST HD';similar=r'US: Example anything \\ WEST HD'
  lm=FuzzyMatcher(match_threshold=threshold);lm.precompute_normalizations([exact,similar])
  # Both normalize into an admitted alias, but only the exact literal is vetoed.
  remaining=lm.match_all_streams('Literal',[exact,similar],{'Literal':[exact,similar]},lineup_country='US',excluded_aliases=literal['excluded_aliases'])
  assert exact not in [r[0] for r in remaining] and similar in [r[0] for r in remaining],remaining
 print('Inventory JSON: Travel/FX opposing-feed fallback blocked at70/95; proper HBO/Showtime West targets and explicit Cinemax East confirmation preserved; literal wildcard/backslash controls passed.')
