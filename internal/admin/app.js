const $ = id => document.getElementById(id);
let documents = [];
let uploading = false;
let syncing = false;
let poll;
let previewRequest = 0;
const date = value => value ? new Date(value).toLocaleDateString(undefined, {day:'numeric',month:'short',year:'numeric'}) : '—';
const size = value => value < 1024 ? `${value} B` : `${(value / 1024).toFixed(1)} KB`;
function notice(message, error = false) {
  $('notice').textContent = message;
  $('notice').className = `notice${error ? ' error' : ''}`;
  $('notice').hidden = false;
}
async function request(url, options) {
  const response = await fetch(url, options);
  const type = response.headers.get('content-type') || '';
  const data = type.includes('application/json') ? await response.json() : {error: await response.text()};
  if (!response.ok) throw new Error(data.error || data.message || `Request failed (${response.status}).`);
  return data;
}
function render() {
  const query = $('search').value.trim().toLowerCase();
  const visible = documents.filter(doc => `${doc.name} ${doc.metadata.owner || ''} ${(doc.metadata.allowed_groups || []).join(' ')}`.toLowerCase().includes(query));
  $('documents').replaceChildren();
  $('count').textContent = visible.length;
  for (const doc of visible) {
    const row = document.createElement('tr');
    const nameCell = document.createElement('td');
    const cell = document.createElement('div'); cell.className = 'document-cell';
    const extension = doc.name.split('.').pop().toUpperCase();
    const info = document.createElement('div');
    const name = document.createElement('span'); name.className = 'document-name'; name.textContent = doc.name;
    const details = document.createElement('div'); details.className = 'file-details'; details.textContent = `${extension} · ${size(doc.size)}`;
    info.append(name, details); cell.append(info); nameCell.append(cell);
    const owner = document.createElement('td'); owner.className = 'owner-cell'; owner.textContent = doc.metadata.owner || '—';
    const access = document.createElement('td');
    const groups = doc.metadata_error ? [doc.metadata_error] : doc.metadata.unrestricted ? ['All groups'] : doc.metadata.allowed_groups || [];
    for (const group of groups) {const badge = document.createElement('span'); badge.className = `badge${doc.metadata_error ? ' error' : ''}`; badge.textContent = group; access.append(badge);}
    const updated = document.createElement('td'); updated.className = 'updated'; updated.textContent = date(doc.updated);
    const action = document.createElement('td'); const view = document.createElement('button'); view.className = 'view-button'; view.textContent = 'View'; view.setAttribute('aria-label', `View ${doc.name}`); view.addEventListener('click', () => preview(doc)); action.append(view);
    row.append(nameCell, owner, access, updated, action); $('documents').append(row);
  }
  $('library-state').className = 'library-state';
  $('library-state').hidden = visible.length > 0;
  $('library-state').textContent = documents.length ? 'No documents match your search.' : 'No documents. Upload a file to begin.';
}
async function loadDocuments() {
  $('refresh').disabled = true;
  try {
    documents = await request('/api/documents');
    $('total').textContent = documents.length;
    $('restricted').textContent = documents.filter(doc => !doc.metadata_error && !doc.metadata.unrestricted).length;
    render();
  } catch (error) {
    $('library-state').hidden = false;
    $('library-state').className = 'library-state error';
    $('library-state').textContent = `Could not load documents. ${error.message}`;
    notice(error.message, true);
  } finally { $('refresh').disabled = false; }
}
async function preview(doc) {
  const current = ++previewRequest;
  $('preview-title').textContent = doc.name; $('preview-path').textContent = doc.key; $('preview-text').textContent = 'Loading…';
  $('preview').showModal();
  try { const result = await request(`/api/document?key=${encodeURIComponent(doc.key)}`); if (current === previewRequest) $('preview-text').textContent = result.text; }
  catch (error) { if (current === previewRequest) $('preview-text').textContent = error.message; }
}
function updateButtons() {
  $('sync').disabled = uploading || syncing;
  $('upload').disabled = uploading || syncing;
  $('upload').textContent = uploading ? 'Uploading…' : 'Upload';
}
async function loadStatus() {
  try {
    const state = await request('/api/sync');
    const wasSyncing = syncing; syncing = state.status === 'running';
    const labels = {idle:'Not run',running:'In progress',complete:'Complete',failed:'Needs attention'};
    $('sync-label').textContent = labels[state.status] || state.status;
    $('sync-label').className = `status-${state.status}`;
    $('sync-time').textContent = state.finished ? `Finished ${new Date(state.finished).toLocaleTimeString([], {hour:'2-digit',minute:'2-digit'})}` : '';
    $('sync-progress').hidden = !syncing;
    $('sync-message').textContent = state.message;
    updateButtons();
    if (syncing) {clearTimeout(poll); poll = setTimeout(loadStatus, 2500);}
    else {
      clearTimeout(poll);
      if (state.status === 'complete' || state.status === 'failed') notice(state.message, state.status === 'failed');
      if (wasSyncing) loadDocuments();
    }
  } catch (error) {notice(`Could not read sync status. ${error.message}`, true); clearTimeout(poll); poll = setTimeout(loadStatus, 5000);}
}
$('upload-form').addEventListener('submit', async event => {
  event.preventDefault();
  if (!$('upload-form').reportValidity()) return;
  const file = $('file').files[0];
  if (!file || file.size > 8 * 1024 * 1024) {notice('Select a file no larger than 8 MB.', true); return;}
  uploading = true; updateButtons();
  const form = new FormData($('upload-form')); form.set('unrestricted', String($('unrestricted').checked));
  try {
    const result = await request('/api/upload', {method:'POST', body:form});
    notice(result.message); $('file').value = ''; await loadDocuments();
  } catch (error) {notice(error.message, true);}
  finally {uploading = false; updateButtons();}
});
$('sync').addEventListener('click', async () => {
  syncing = true; updateButtons();
  try {await request('/api/sync', {method:'POST'}); await loadStatus();}
  catch (error) {syncing = false; updateButtons(); notice(error.message, true);}
});
$('unrestricted').addEventListener('change', () => {$('groups').disabled = $('unrestricted').checked; $('groups').required = !$('unrestricted').checked;});
$('search').addEventListener('input', render);
$('refresh').addEventListener('click', loadDocuments);
$('close-preview').addEventListener('click', () => {$('preview').close();});
loadDocuments(); loadStatus();
