// Run with node tools/test_provider_address_ui.cjs. No browser or external API.
const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const html = fs.readFileSync('lineuparr.html','utf8');
const script = html.match(/<script>([\s\S]*?)<\/script>/)[1];
new vm.Script(script);
function declaration(name) {
 const start = script.search(new RegExp(`    (?:async )?function ${name}\\(`));
 assert.ok(start >= 0, name);
 const rest = script.slice(start);
 const next = rest.slice(1).search(/\n    (?:async )?function /);
 return next < 0 ? rest : rest.slice(0,next+1);
}
const controls = {postalScan:{},providerAddressForget:{},providerAddressQuery:{},providerAddressResults:{replaceChildren(){}}};
const context = vm.createContext({els:controls, Object,String,JSON, addressSaving:false,scanStarting:false,scanRequestError:'',selectedProviderAddress:null,providerAddressConfig:{required:true,fingerprint:'source'},document:{getElementById(){return {open:false}}},setMarketMessage(text){context.message=text},setProviderAddressStatus(text){context.addressMessage=text},loadAliasIndex:async()=>{},initProviderAddress:async()=>{}});
for (const name of ['addressFields','saveProviderAddress','startAliasScan']) vm.runInContext(declaration(name),context);
(async()=>{
 let payload;
 context.api=async(path,options)=>{payload=JSON.parse(options.body);return {started:true}};
 await context.saveProviderAddress({id:'geocoder-only',formattedAddress:'1 Test Street',streetAddress:'1 Test Street',postalCode:'11743',countryCode:'US'});
 assert.equal(payload.address.id,undefined);
 assert.equal(payload.fingerprint,'source');
 assert.ok(context.selectedProviderAddress);
 await context.startAliasScan('postal');
 assert.equal(payload.action,'postal');
 assert.equal(payload.sourceFingerprint,'source');
 assert.equal(payload.providerAddress,undefined);
 context.api=async()=>{throw new Error('New actionable failure')};
 await context.startAliasScan('postal');
 assert.equal(context.scanRequestError,'New actionable failure');
 assert.equal(context.scanStarting,false);
 assert.ok(script.includes("if (scanRequestError) setMarketMessage(scanRequestError, 'error')"));
 assert.ok(html.indexOf('id="alias-panel"') < html.indexOf('id="provider-address"'));
 assert.ok(html.indexOf('id="provider-address"') < html.indexOf('id="postal-scan"'));
 console.log('Provider address UI regression checks passed');
})().catch(error=>{console.error(error);process.exitCode=1});
