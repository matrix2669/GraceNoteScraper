const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const script=fs.readFileSync('setup.html','utf8').split('<script>')[1].split('</script>')[0];new vm.Script(script);
const start=script.indexOf('      function setXMLTVCopyStatus('),end=script.indexOf("      fetch('/api/setup/share-links'",start);
const nodes=new Map();let copied='';
const node=id=>{if(!nodes.has(id))nodes.set(id,{value:'',style:{},textContent:'',addEventListener(){},setAttribute(){},select(){copied=this.value}});return nodes.get(id)};
const c=vm.createContext({URL,xmltvGuideURL:'https://guide.example/xmlguide.xmltv',xmltvGuideLink:node('xmltvGuideLink'),xmltvCopyStatus:node('status'),
 document:{getElementById:node,createElement:()=>node('textarea'),execCommand:()=>true,body:{appendChild(){},removeChild(){}}},
 window:{isSecureContext:false},navigator:{}});
vm.runInContext(script.slice(start,end),c);
c.renderInternalLinks({internalBaseURL:'http://gracenote-dev:8080'});assert.equal(node('internalXMLTVRow').hidden,false);
c.copyXMLTVGuideURL(node('internalXMLTVLink').textContent);assert.equal(copied,'http://gracenote-dev:8080/xmlguide.xmltv');
c.copyXMLTVGuideURL({type:'click'});assert.equal(copied,'https://guide.example/xmlguide.xmltv');
c.renderInternalLinks({internalBaseURL:''});assert.equal(node('internalXMLTVRow').hidden,true);
console.log('Internal XMLTV visibility and HTTP clipboard fallback passed');
