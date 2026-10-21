#!/usr/bin/env python3
"""Build portable aliases from pinned public station registries, never positions.

Inputs are local copies of the exact revisions recorded below. No acquisition,
provider/account data or mutable runtime state is read by this generator.
"""
import argparse, csv, hashlib, io, json, re
from pathlib import Path
from generate_prismcast_catalog import top_level_definitions, clean_aliases
from generate_pbs_catalog import service_identity
PRISM='0aed952e80ec6a1bd997a7cb16a3abe256bf253c'
PBS='b651b3e14fb0f8d9b01610cd221b60285b4c2812'
# SHA256 of the original Git blobs at those revisions (including CSV CRLF/BOM).
# A changed input requires an explicit provenance review, not a new self-label.
PINNED_INPUT_SHA256 = {
    'prismcast': '9072bba19081f7204f6b5b93f4a5908343c4383dcfa8c75b63699c4059da3408',
    'pbsStations': '48401d03baa12ecd1eae973f8df03a5a5f8d5eea63dc63c15a953c854d16efb9',
    'pbsMapping': '419e9a0556a3537d225c8558ac4baf72a3396af5517e3fd0658add071a96fd9f',
}

def pinned_text(key, path):
    data = Path(path).read_bytes()
    if hashlib.sha256(data).hexdigest() != PINNED_INPUT_SHA256[key]:
        raise ValueError(f'{key}: input does not match the reviewed pinned Git blob')
    return data.decode('utf-8')

def root_value(block, field):
    # The pinned registry has four-space root fields. Never consume a nested
    # service's stationId/name as the canonical channel identity.
    match = re.search(rf'(?m)^    {re.escape(field)}: "([^"\n]+)"', block)
    return match[1] if match else ''

def generate(prism, stations, mapping):
    rows=[]
    for key, block in top_level_definitions(prism):
        name=root_value(block,'name'); sid=root_value(block,'stationId'); pac=root_value(block,'pacificStationId')
        if not name or not sid: continue
        # The pinned registry documents stationId as East when pacificStationId
        # creates a sibling. Manual Pacific definitions declare their own name.
        for field, identifier in [('stationId',sid),('pacificStationId',pac)]:
            if not identifier: continue
            region='west' if field=='pacificStationId' else 'east' if pac else ''
            base=re.sub(r'\s*\((?:Pacific|West|East)\)\s*$', '', name, flags=re.I)
            if re.search(r'\((?:Pacific|West)\)$',name,re.I): region='west'
            if re.search(r'\(East\)$',name,re.I): region='east'
            canonical=base+' - '+region.title() if region else name
            aliases=[canonical]
            for a in clean_aliases(name,block):
                # Provider position selectors are not portable names. Numeric
                # service brands remain intact: only trailing position forms drop.
                if re.search(r'\b(?:HD|SD)\s*\d{2,}\s*$',a,re.I) or re.match(r'^\d+\s',a): continue
                tagged=re.search(r'\b(East|West|Pacific)\b',a,re.I)
                if tagged and not region: continue
                if tagged and region and ('west' if tagged[1].lower() in ('west','pacific') else 'east')!=region: continue
                # Never inherit ambiguous generic selectors onto a Pacific sibling.
                if region=='west' and not tagged: continue
                aliases.append(a)
            rows.append(dict(stationId=identifier,name=canonical,aliases=sorted(set(aliases)),sourceId='prismcast-network-catalog',revision=PRISM,rowId=key+':'+field,path='src/channels/index.ts'))
    by_station={r['channel_id']:r for r in csv.DictReader(io.StringIO(stations.lstrip('\ufeff')))}
    for row in csv.DictReader(io.StringIO(mapping.lstrip('\ufeff'))):
        key=row['channel_id'].strip(); root=key.split('_ga_',1)[0].split('_kids_',1)[0]
        # The map supplies an ID, but no distinct service descriptor for
        # arbitrary local subchannels. A synthesized parent-station label would
        # collapse different digital services into exact positive aliases.
        if '_ga_local_subchannel_' in key: continue
        name, aliases, _=service_identity(key,by_station.get(root,{}))
        # Local OTA positions are display metadata, never reusable aliases.
        values=[v.strip() for v in [name,*aliases] if v.strip() and not re.search(r'\[|\]|\b\d+\.\d+\b',v)]
        if not values: continue
        for field in ('gracenote_id','gracenote_id_natural'):
            sid=row.get(field,'').strip()
            if not sid.isdigit(): continue
            # A mapped fallback ID identifies the national service, not the
            # local affiliate. This holds even if only one local row uses it.
            admitted = values
            natural = row.get('gracenote_id_natural', '').strip()
            if field == 'gracenote_id' and sid != natural:
                known_service = key.endswith(('_kids_main', '_ga_create', '_ga_nhk', '_ga_world', '_ga_fnx'))
                admitted = [name] if known_service else []
            if admitted:
                rows.append(dict(stationId=sid,name=name,aliases=sorted(set(admitted)),sourceId='pbs-gracenote-station-map',revision=PBS,rowId=key+':'+field,path='executables/pbs_gracenote_map.csv'))
    # Shared national/fallback PBS IDs cannot carry each local market's callsign.
    local_owners={}
    for r in rows:
        if r['sourceId']=='pbs-gracenote-station-map':
            root=r['rowId'].split(':')[0].split('_ga_',1)[0].split('_kids_',1)[0]
            local_owners.setdefault(r['stationId'],set()).add(root)
    for r in rows:
        if r['sourceId']=='pbs-gracenote-station-map' and len(local_owners[r['stationId']])>1:
            r['aliases']=[r['name']] if not re.search(r'\b[WK][A-Z]{2,3}\b',r['name']) else []
    # Keep all original claims for a conflict census. A station ID with multiple
    # different services is quarantined, rather than last-writer-wins.
    conflicts={r['stationId'] for r in rows if len({q['name'].casefold() for q in rows if q['stationId']==r['stationId']})>1}
    # A generic alias shared by distinct service descriptors must not become a
    # station-specific assertion just because only one target is currently shown.
    owners={}
    for r in rows:
        for a in r['aliases']: owners.setdefault(a.casefold(),set()).add(r['name'].casefold())
    for r in rows: r['aliases']=[a for a in r['aliases'] if len(owners[a.casefold()])==1]
    rows=[r for r in rows if r['aliases'] and r['stationId'] not in conflicts]
    return dict(schemaVersion=1,sources=[dict(id='prismcast-network-catalog',url='https://github.com/hjdhjd/prismcast',revision=PRISM,license='ISC'),dict(id='pbs-gracenote-station-map',url='https://github.com/babsonnexus/stream-link-manager-for-channels',revision=PBS,license='MIT')],inputs={k:hashlib.sha256(v.encode()).hexdigest() for k,v in [('prismcast',prism),('pbsStations',stations),('pbsMapping',mapping)]},quarantinedStationIds=sorted(conflicts),channels=sorted(rows,key=lambda r:(r['stationId'],r['rowId'])))
def main():
    p=argparse.ArgumentParser();p.add_argument('--prismcast',required=True);p.add_argument('--pbs-stations',required=True);p.add_argument('--pbs-mapping',required=True);p.add_argument('--output',required=True);a=p.parse_args()
    result=generate(pinned_text('prismcast',a.prismcast),pinned_text('pbsStations',a.pbs_stations),pinned_text('pbsMapping',a.pbs_mapping))
    result['inputGitBlobSHA256'] = PINNED_INPUT_SHA256
    Path(a.output).write_text(json.dumps(result,indent=2,ensure_ascii=False)+'\n')
if __name__=='__main__':main()
