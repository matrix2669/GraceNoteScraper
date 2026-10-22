// Read the production guard/action component for isolated function fixtures.
module.exports = html => html.slice(html.indexOf('    function matchActionState('), html.indexOf('    function renderMatchCoverage('));
