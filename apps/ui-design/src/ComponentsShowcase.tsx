import { useEffect, useRef, useState } from 'react';
import { Archive, ArrowRight, Check, Copy, FileText, Folder, HelpCircle, LayoutGrid, List, MoreHorizontal, Pencil, Plus, RefreshCw, Share2, WifiOff } from 'lucide-react';
import { Alert, Badge, Breadcrumb, Button, Card, Checkbox, DateTimePicker, Dialog, Drawer, DropdownMenu, EmptyState, IconButton, Logo, Pagination, Popover, Progress, RadioGroup, RangeSlider, ResourceCard, SearchField, SegmentedControl, Select, Skeleton, Switch, Table, Tabs, TaskCard, Textarea, TextField, Tooltip, useToast, useUILocale } from '@molr/ui';
import type { TableColumn, TableSort, Theme } from '@molr/ui';
import { libraryMessages } from './library-messages';
import type { Locale, Messages } from './messages';
import { ComponentExample as Example } from './ComponentExample';
import { ChatShowcase } from './ChatShowcase';
import { CodeBlockShowcase } from './CodeBlockShowcase';
import { SkeletonShowcase } from './SkeletonShowcase';
import { ConnectionNoticeShowcase } from './ConnectionNoticeShowcase';

type TextKey = { [Key in keyof typeof libraryMessages.en]: typeof libraryMessages.en[Key] extends string ? Key : never }[keyof typeof libraryMessages.en];
function DemoText({ name }: { name: TextKey }) { const { locale } = useUILocale(); return libraryMessages[locale][name]; }

const records = [
  { id: 'guide', type: 'documents', size: 248, status: 'synced' },
  { id: 'logo', type: 'images', size: 64, status: 'synced' },
  { id: 'research', type: 'documents', size: 512, status: 'syncing' },
  { id: 'palette', type: 'images', size: 128, status: 'synced' },
  { id: 'icons', type: 'folders', size: 2048, status: 'queued' },
  { id: 'interface', type: 'images', size: 1536, status: 'synced' },
  { id: 'notes', type: 'documents', size: 96, status: 'synced' },
  { id: 'archive', type: 'folders', size: 4096, status: 'queued' },
] as const;
type Resource = (typeof records)[number];

export function ComponentsShowcase({ locale, copy, theme }: { locale: Locale; copy: Messages; theme: Theme }) {
  const t = libraryMessages[locale];
  const { toast } = useToast();
  const [category, setCategory] = useState(() => { const requested = new URLSearchParams(window.location.search).get('components'); return requested === 'chat' || requested === 'feedback' ? requested : 'foundations'; });
  const [range, setRange] = useState<[number, number]>([20, 80]);
  const [eventRange, setEventRange] = useState<[number, number]>([3, 8]);
  const [expiry, setExpiry] = useState<string | null>(null);
  const [expirySizes, setExpirySizes] = useState<{ sm: string | null; lg: string | null }>({ sm: null, lg: null });
  const [name, setName] = useState('');
  const [notes, setNotes] = useState('');
  const [nameError, setNameError] = useState(false);
  const [saving, setSaving] = useState(false);
  const [notifications, setNotifications] = useState(true);
  const [switchSizes, setSwitchSizes] = useState({ sm: false, md: false, lg: false });
  const [files, setFiles] = useState(['guide']);
  const [frequency, setFrequency] = useState('daily');
  const [layout, setLayout] = useState('list');
  const [sortOrder, setSortOrder] = useState('updated');
  const [defaultView, setDefaultView] = useState('');
  const [searchResource, setSearchResource] = useState('');
  const [searchResources, setSearchResources] = useState<string[]>([]);
  const [tab, setTab] = useState('overview');
  const [segmentedTab, setSegmentedTab] = useState('overview');
  const [pageExample, setPageExample] = useState(1);
  const [showAlert, setShowAlert] = useState(true);
  const [retrying, setRetrying] = useState(false);
  const [retried, setRetried] = useState(false);
  const [archived, setArchived] = useState(false);
  const [emptyMode, setEmptyMode] = useState('empty');
  const [loaded, setLoaded] = useState(false);
  const [progress, setProgress] = useState(0);
  const [running, setRunning] = useState(false);
  const [dialog, setDialog] = useState(false);
  const [creating, setCreating] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [drawer, setDrawer] = useState(false);
  const [draftName, setDraftName] = useState('');
  const [draftNotes, setDraftNotes] = useState('');
  const [draftError, setDraftError] = useState(false);
  const [visibility, setVisibility] = useState('private');
  const [query, setQuery] = useState('');
  const [page, setPage] = useState(1);
  const [sort, setSort] = useState<TableSort>({ column: 'name', direction: 'asc' });
  const [selected, setSelected] = useState<string[]>([]);
  const [resource, setResource] = useState<Resource | null>(null);
  const timers = useRef(new Set<ReturnType<typeof setTimeout>>());
  useEffect(() => { const pending = timers.current; return () => { for (const timer of pending) clearTimeout(timer); }; }, []);
  function later(callback: () => void, delay = 1000) { const timer = setTimeout(() => { timers.current.delete(timer); callback(); }, delay); timers.current.add(timer); }
  useEffect(() => {
    if (!running) return;
    const timer = setInterval(() => setProgress(value => Math.min(100, value + 10)), 600);
    return () => clearInterval(timer);
  }, [running]);
  useEffect(() => { if (progress === 100) setRunning(false); }, [progress]);
  function notify(key: TextKey, tone: 'positive' | 'danger' | 'neutral' = 'positive') { toast({ title: <DemoText name={key} />, tone }); }
  function archive() {
    setArchived(true);
    toast({ title: <DemoText name="archiveTitle" />, description: <DemoText name="archiveBody" />, action: { label: <DemoText name="undo" />, onClick: () => { setArchived(false); notify('undone'); } } });
  }
  function edit() { setCreating(false); setDraftName(name || t.currentProject); setDraftNotes(notes); setDraftError(false); setDialog(true); }
  function create() { setCreating(true); setDraftName(''); setDraftNotes(''); setDraftError(false); setDialog(true); }
  const tabItems = [
    { value: 'overview', label: t.overview, content: <p className="component-copy">{t.overviewText}</p> },
    { value: 'activity', label: t.activity, content: <p className="component-copy component-copy--lines">{t.activityText}</p> },
    { value: 'settings', label: t.settings, content: <><p className="component-copy">{t.settingsText}</p><Switch label={t.notifications} checked={notifications} onChange={event => setNotifications(event.target.checked)} /></> },
    { value: 'planned', label: t.planned, content: null, disabled: true },
  ];
  const filtered = records.filter(item => `${t.resources[item.id]} ${t[item.type]}`.toLocaleLowerCase(locale).includes(query.trim().toLocaleLowerCase(locale))).sort((a, b) => {
    const result = sort.column === 'size' ? a.size - b.size : t.resources[a.id].localeCompare(t.resources[b.id], locale);
    return sort.direction === 'asc' ? result : -result;
  });
  const totalPages = Math.max(1, Math.ceil(filtered.length / 4));
  const currentPage = Math.min(page, totalPages);
  const rows = filtered.slice((currentPage - 1) * 4, currentPage * 4);
  const columns: TableColumn<Resource>[] = [
    { key: 'name', header: t.resourceName, sortable: true, cell: item => <span className="resource-name">{item.type === 'folders' ? <Folder size={16} /> : <FileText size={16} />}{t.resources[item.id]}</span> },
    { key: 'type', header: t.resourceType, cell: item => t[item.type] },
    { key: 'size', header: t.resourceSize, sortable: true, cell: item => `${item.size} KB` },
    { key: 'status', header: t.resourceStatus, cell: item => <Badge tone={item.status === 'synced' ? 'positive' : 'neutral'}>{t[item.status]}</Badge> },
    { key: 'actions', header: t.resourceActions, align: 'end', cell: item => <Button size="sm" variant="ghost" onClick={() => { setResource(item); setDrawer(true); }}>{t.openDrawer}</Button> },
  ];
  const taskStatus = progress === 100 ? t.complete : running ? t.active : progress > 0 ? t.paused : t.pending;
  const taskActions = <><Button size="sm" variant="secondary" onClick={() => { if (progress === 100) setProgress(0); setRunning(value => !value); }}>{running ? t.pause : progress === 100 ? t.reset : progress > 0 ? t.resume : t.start}</Button>{progress > 0 && progress < 100 && <Button size="sm" variant="ghost" onClick={() => { setProgress(0); setRunning(false); }}>{t.reset}</Button>}</>;
  const emptyContent = emptyMode === 'empty' ? <EmptyState title={t.emptyProjects} description={t.emptyProjectsBody} illustration={<Logo markOnly variant={theme === 'dark' ? 'light' : 'dark'} animation="curious" />} action={<Button size="sm" onClick={create}><Plus />{t.newProject}</Button>} />
    : emptyMode === 'search' ? <EmptyState title={t.noResults} description={t.noResultsBody} illustration={<FileText />} action={<Button size="sm" variant="secondary" onClick={() => setEmptyMode('empty')}>{t.clearSearch}</Button>} />
    : <EmptyState title={t.offline} description={t.offlineBody} illustration={<WifiOff />} action={<Button size="sm" variant="secondary" onClick={() => { setEmptyMode('empty'); notify('connected'); }}><RefreshCw />{t.reconnect}</Button>} />;

  return <div className="component-library">
    <p className="component-library__intro">{t.intro}</p>
    <Tabs label={t.categories} value={category} onValueChange={setCategory} items={[
      { value: 'foundations', label: t.foundations, content: <div className="component-foundations">
        <Card className="type-specimen"><div className="specimen-label"><span>TYPOGRAPHY</span><span>Aa / <span lang="zh-CN">字</span></span></div><p className="type-specimen__display">Stay<br />curious<span>.</span></p><p className="type-specimen__motto">{copy.components.typography.motto}</p><p className="type-specimen__description">{copy.components.typography.description}<br />{copy.components.typography.descriptionEnd}</p><div className="type-scale"><span>{copy.components.typography.heading}<b>32 / 600</b></span><span>{copy.components.typography.body}<b>16 / 400</b></span><span>{copy.components.typography.caption}<b>12 / 400</b></span></div></Card>
        <Card className="rules-specimen"><div className="specimen-label"><span>SHAPE & RHYTHM</span><span>4 / 8 / 16</span></div><div className="shape-row"><div><span className="shape shape--control" /><span>{copy.components.shape.control}<b>8 px</b></span></div><div><span className="shape shape--card" /><span>{copy.components.shape.card}<b>16 px</b></span></div><div><span className="shape shape--pill" /><span>{copy.components.shape.badge}<b>Full</b></span></div></div><div className="rhythm-row"><span>{copy.components.shape.spacing}</span><span className="mono">4 · 8 · 12 · 16 · 24 · 32</span></div><div className="component-statuses"><p className="control-label">{copy.components.status.label}</p><div className="component-row"><Badge>{copy.components.status.neutral}</Badge><Badge tone="positive">{copy.components.status.positive}</Badge><Badge tone="warning">{copy.components.status.warning}</Badge><Badge tone="danger">{copy.components.status.danger}</Badge></div><div className="component-row"><Badge size="sm">{copy.components.status.small}</Badge><Badge size="sm" dot={false}>{copy.components.status.noDot}</Badge></div><p className="subtle-copy">{copy.components.status.note}</p></div></Card>
        <CodeBlockShowcase />
      </div> },
      { value: 'input', label: t.input, content: <div className="component-library__grid">
        <Example title={t.buttons} description={t.buttonsNote}>
          <div className="component-row"><Button onClick={() => notify('buttonHint', 'neutral')}>{t.primary}<ArrowRight /></Button><Button variant="secondary" onClick={() => notify('buttonHint', 'neutral')}>{t.secondary}</Button><Button variant="ghost" onClick={() => notify('buttonHint', 'neutral')}>{t.textButton}</Button></div>
          <div className="component-row"><Button variant="accent" onClick={() => notify('buttonHint', 'neutral')}><Plus />{t.accent}</Button><Button variant="danger" onClick={() => setConfirm(true)}>{t.danger}</Button><Button disabled>{t.disabled}</Button></div>
          <div className="component-row"><Button size="sm" onClick={() => notify('buttonHint', 'neutral')}>{t.smallButton}</Button><Button size="lg" variant="secondary" onClick={() => notify('buttonHint', 'neutral')}>{t.largeButton}</Button><Tooltip content={t.tooltip}><IconButton label={t.help}><HelpCircle /></IconButton></Tooltip></div>
        </Example>
        <Example title={t.forms} description={t.formsNote}>
          <form className="component-form" onSubmit={event => { event.preventDefault(); if (!name.trim()) { setNameError(true); return; } setSaving(true); later(() => { setSaving(false); notify('saved'); }); }}>
            <TextField label={t.name} value={name} onChange={event => { setName(event.target.value); setNameError(false); }} placeholder={t.namePlaceholder} description={t.nameDescription} error={nameError ? t.nameError : undefined} maxLength={60} />
            <Textarea label={t.notes} value={notes} onChange={event => setNotes(event.target.value)} placeholder={t.notesPlaceholder} description={t.notesDescription} rows={3} maxLength={500} />
            <div className="component-row component-row--spread"><Switch label={t.notifications} checked={notifications} onChange={event => setNotifications(event.target.checked)} /><Button type="submit" variant="secondary" loading={saving}>{saving ? t.saving : t.submit}</Button></div>
          </form>
        </Example>
        <Example id="range-slider" title={t.rangeSlider} description={t.rangeSliderNote}>
          <RangeSlider label={t.rangeValue} min={0} max={100} step={5} value={range} onValueChange={setRange} formatValue={value => `${value}%`} description={t.rangeKeyboard} />
          <div className="component-row"><Button size="sm" variant="secondary" onClick={() => setRange([50, 50])}>{t.rangeOverlap}</Button><Button size="sm" variant="ghost" onClick={() => setRange([20, 80])}>{t.reset}</Button></div>
          <RangeSlider label={t.rangeEvents} size="sm" min={1} max={12} value={eventRange} onValueChange={setEventRange} lowerLabel={t.rangeFirst} upperLabel={t.rangeLast} formatValue={value => `#${value}`} />
          <RangeSlider label={t.rangeDisabled} value={[25, 75]} onValueChange={() => {}} disabled />
        </Example>
        <Example title={t.choices} description={t.choicesNote}>
          <Checkbox label={t.mixed} checked={files.length === 2} indeterminate={files.length > 0 && files.length < 2} onChange={event => setFiles(event.target.checked ? ['guide', 'logo'] : [])} />
          <div className="component-inset">{(['guide', 'logo'] as const).map(key => <Checkbox key={key} label={t.resources[key]} checked={files.includes(key)} onChange={event => setFiles(current => event.target.checked ? [...current, key] : current.filter(file => file !== key))} />)}<Checkbox label={t.readOnly} checked disabled /></div>
          <RadioGroup label={t.delivery} value={frequency} onValueChange={setFrequency} options={[{ value: 'immediate', label: t.immediate }, { value: 'daily', label: t.daily }, { value: 'weekly', label: t.weekly, description: t.unavailable, disabled: true }]} />
          <SegmentedControl label={t.layout} value={layout} onValueChange={setLayout} options={[{ value: 'list', label: t.list, icon: <List /> }, { value: 'grid', label: t.grid, icon: <LayoutGrid /> }, { value: 'board', label: t.board, disabled: true }]} />
        </Example>
        <Example title={t.selects} description={t.selectsNote}>
          <Select label={copy.components.select.sort} name="sortOrder" options={[
            { value: 'updated', label: copy.components.select.updated, description: copy.components.select.updatedDescription },
            { value: 'name', label: copy.components.select.name, description: copy.components.select.nameDescription },
            { value: 'created', label: copy.components.select.created, description: copy.components.select.createdDescription },
            { value: 'manual', label: copy.components.select.manual, description: copy.components.select.manualDescription, disabled: true },
          ]} value={sortOrder} onValueChange={setSortOrder} description={copy.components.select.sortDescription} />
          <Select label={copy.components.select.view} options={[{ value: 'overview', label: t.overview }, { value: 'list', label: t.list }, { value: 'board', label: t.board }]} value={defaultView} onValueChange={setDefaultView} placeholder={copy.components.select.viewPlaceholder} error={defaultView ? undefined : copy.components.select.viewError} />
          <Select label={copy.components.select.permissions} options={[{ value: 'viewer', label: copy.components.select.viewer }]} value="viewer" onValueChange={() => {}} disabled description={copy.components.select.disabledDescription} />
        </Example>
        <Example title={t.searchableSelect} description={t.searchableSelectNote}>
          <Select size="sm" searchable label={t.searchSingle} value={searchResource} onValueChange={setSearchResource} options={records.map(item => ({ value: item.id, label: t.resources[item.id], description: t[item.type], disabled: item.id === 'icons' }))} />
          <Select size="sm" searchable multiple label={t.searchMultiple} value={searchResources} onValueChange={setSearchResources} options={records.map(item => ({ value: item.id, label: t.resources[item.id], description: t[item.type], disabled: item.id === 'icons' }))} />
        </Example>
        <Example title={t.dateTimePicker} description={t.dateTimePickerNote} id="date-time-picker-example">
          <DateTimePicker label={t.expiry} value={expiry} onValueChange={setExpiry} />
          <DateTimePicker size="sm" label={t.smallExpiry} value={expirySizes.sm} onValueChange={sm => setExpirySizes(values => ({ ...values, sm }))} />
          <DateTimePicker size="lg" label={t.largeExpiry} value={expirySizes.lg} onValueChange={lg => setExpirySizes(values => ({ ...values, lg }))} />
          <DateTimePicker size="sm" label={t.disabledExpiry} value={null} onValueChange={() => {}} disabled />
          <output hidden data-testid="expiry-value">{expiry ?? t.noExpiry}</output>
        </Example>
        <Example title={t.switchSizes} description={t.switchSizesNote}>
          {(['sm', 'md', 'lg'] as const).map(size => <Switch key={size} size={size} label={size === 'sm' ? t.switchSmall : size === 'md' ? t.switchMedium : t.switchLarge} checked={switchSizes[size]} onChange={event => setSwitchSizes(values => ({ ...values, [size]: event.target.checked }))} />)}
        </Example>
      </div> },
      { value: 'chat', label: t.chat, content: <ChatShowcase active={category === 'chat'} /> },
      { value: 'navigation', label: t.navigation, content: <div className="component-library__grid">
        <Example title={t.tabs} description={t.tabsNote}><div className="tab-variant-example"><p className="control-label">{t.lineTabs}</p><Tabs label={t.tabExample} value={tab} onValueChange={setTab} items={tabItems} /></div><div className="tab-variant-example"><p className="control-label">{t.segmentedTabs}</p><Tabs label={`${t.tabExample} · ${t.segmentedTabs}`} variant="segmented" value={segmentedTab} onValueChange={setSegmentedTab} items={tabItems} /></div></Example>
        <Example title={t.navigationTools} description={t.navigationNote}><Breadcrumb items={[{ label: t.home, href: '#main' }, { label: t.projects, href: '#components' }, { label: t.currentProject }]} /><div className="pagination-preview"><span className="pagination-preview__number">{String(pageExample).padStart(2, '0')}</span><p>{t.pageDescription(pageExample)}</p></div><Pagination page={pageExample} totalPages={12} onPageChange={setPageExample} /></Example>
      </div> },
      { value: 'feedback', label: t.feedback, content: <div className="component-library__grid">
        <ConnectionNoticeShowcase />
        <Example title={t.alerts} description={t.alertsNote}><Alert title={t.infoTitle}>{t.infoBody}</Alert><Alert tone="positive" title={t.successTitle}>{t.successBody}</Alert>{showAlert ? <Alert tone="warning" title={t.warningTitle} onDismiss={() => setShowAlert(false)}>{t.warningBody}</Alert> : <Button variant="ghost" onClick={() => setShowAlert(true)}>{t.restore}</Button>}<Alert tone={retried ? 'positive' : 'danger'} title={retried ? t.successTitle : t.errorTitle} action={!retried && <Button size="sm" variant="secondary" loading={retrying} onClick={() => { setRetrying(true); later(() => { setRetrying(false); setRetried(true); }); }}>{t.retry}</Button>}>{retried ? t.successBody : t.errorBody}</Alert></Example>
        <Example title={t.toast} description={t.toastNote}><div className="component-row"><Button variant="secondary" onClick={() => notify('saved')}>{t.showSuccess}</Button><Button variant="secondary" onClick={() => toast({ title: <DemoText name="errorTitle" />, description: <DemoText name="errorBody" />, tone: 'danger', action: { label: <DemoText name="retry" />, onClick: () => notify('saved') } })}>{t.showError}</Button><Button variant="secondary" onClick={archive}>{t.showUndo}</Button></div><Badge tone={archived ? 'neutral' : 'positive'}>{archived ? t.archived : t.active}</Badge></Example>
        <Example title={t.empty} description={t.emptyNote}><SegmentedControl label={t.empty} hideLabel value={emptyMode} onValueChange={setEmptyMode} options={[{ value: 'empty', label: t.empty }, { value: 'search', label: t.noResults }, { value: 'offline', label: t.offline }]} />{emptyContent}</Example>
        <Example title={t.loading} description={t.loadingNote}><Switch label={t.skeletonToggle} checked={!loaded} onChange={event => setLoaded(!event.target.checked)} /><div className="skeleton-preview" role="status" aria-busy={!loaded} aria-label={!loaded ? t.skeleton : t.ready}>{loaded ? <><Check size={24} /><p>{t.ready}</p></> : <><Skeleton shape="circle" width={40} /><div><Skeleton width="75%" /><Skeleton width="100%" /><Skeleton width="55%" /></div></>}</div><Progress label={t.progress} value={progress} /><div className="component-row">{taskActions}</div><Progress label={t.waiting} /></Example>
      </div> },
      { value: 'skeletons', label: t.skeletons, content: <SkeletonShowcase locale={locale} /> },
      { value: 'overlays', label: t.overlays, content: <div className="component-library__grid">
        <Example title={t.dialogs} description={t.dialogsNote}><div className="component-row"><Button onClick={edit}>{t.openDialog}</Button><Button variant="secondary" onClick={() => { setResource(null); setDrawer(true); }}>{t.openDrawer}</Button><Button variant="ghost" onClick={() => setConfirm(true)}>{t.openDanger}</Button></div></Example>
        <Example title={t.menu} description={t.menuNote}><DropdownMenu label={t.menuLabel} trigger={<Button variant="secondary" onKeyDown={event => { if (event.key === 'F2') { event.preventDefault(); edit(); } }}>{t.more}<MoreHorizontal /></Button>} items={[
          { id: 'rename', label: t.rename, icon: <Pencil />, shortcut: 'F2', onSelect: edit },
          { id: 'duplicate', label: t.duplicate, icon: <Copy />, onSelect: () => notify('copied') },
          { id: 'export', label: t.export, disabled: true, onSelect: () => {} },
          { id: 'archive', label: t.openDanger, icon: <Archive />, danger: true, separatorBefore: true, onSelect: () => setConfirm(true) },
        ]} /></Example>
        <Example title={t.hints} description={t.hintsNote} wide><div className="component-row"><Tooltip content={t.tooltip}><IconButton label={t.help}><HelpCircle /></IconButton></Tooltip><Popover title={t.popoverTitle} trigger={<Button variant="secondary"><Share2 />{t.popover}</Button>}><p>{t.popoverBody}</p><RadioGroup label={t.popoverTitle} hideLabel value={visibility} onValueChange={setVisibility} options={[{ value: 'private', label: t.private }, { value: 'team', label: t.team }]} /></Popover></div></Example>
      </div> },
      { value: 'data', label: t.data, content: <div className="component-library__grid">
        <Example title={t.table} description={t.tableNote} wide id="resource-example"><div className="resource-toolbar"><SearchField label={t.search} hideLabel placeholder={t.searchPlaceholder} value={query} onValueChange={value => { setQuery(value); setPage(1); }} /><span role="status">{t.selection(selected.length)}</span>{selected.length > 0 && <Button variant="ghost" size="sm" onClick={() => setSelected([])}>{t.clearSelection}</Button>}</div><Table caption={t.tableCaption} columns={columns} rows={rows} rowKey={item => item.id} sort={sort} onSortChange={value => { setSort(value); setPage(1); }} selection={{ keys: selected, onChange: setSelected, rowLabel: item => t.resources[item.id] }} emptyState={<EmptyState title={t.noResults} description={t.noResultsBody} action={<Button size="sm" variant="secondary" onClick={() => setQuery('')}>{t.clearSearch}</Button>} />} /><Pagination page={currentPage} totalPages={totalPages} onPageChange={setPage} /></Example>
        <Example title={t.resourceCards} description={t.resourceCardsNote}><ResourceCard name={t.currentProject} description={t.resourceDescription} icon={<Folder />} status={{ label: t.synced, tone: 'positive' }} metadata={t.resourceMeta} action={<Button variant="secondary" size="sm" onClick={() => { setQuery(''); setPage(1); document.getElementById('resource-example')?.scrollIntoView({ behavior: 'smooth', block: 'start' }); }}>{t.resourceButton}<ArrowRight /></Button>} /></Example>
        <Example title={t.taskCards} description={t.taskCardsNote}><TaskCard title={t.taskTitle} description={t.taskDescription} status={{ label: taskStatus, tone: progress === 100 ? 'positive' : 'neutral' }} progress={{ label: t.progress, value: progress }} metadata={t.taskMeta} actions={taskActions} /></Example>
      </div> },
    ]} />
    <Dialog open={dialog} onOpenChange={setDialog} title={creating ? t.newProject : t.dialogTitle} description={creating ? t.createDescription : t.dialogDescription} footer={<><Button variant="ghost" disabled={saving} onClick={() => setDialog(false)}>{t.cancel}</Button><Button form="edit-project-form" type="submit" loading={saving}>{saving ? t.saving : t.save}</Button></>} dismissible={!saving}>
      <form id="edit-project-form" className="component-form" onSubmit={event => { event.preventDefault(); if (!draftName.trim()) { setDraftError(true); return; } setSaving(true); later(() => { setName(draftName.trim()); setNotes(draftNotes); setSaving(false); setDialog(false); notify('saved'); }); }}><TextField label={t.name} value={draftName} onChange={event => { setDraftName(event.target.value); setDraftError(false); }} error={draftError ? t.nameError : undefined} maxLength={60} /><Textarea label={t.notes} value={draftNotes} onChange={event => setDraftNotes(event.target.value)} maxLength={500} /></form>
    </Dialog>
    <Dialog open={confirm} onOpenChange={setConfirm} title={t.dangerTitle} description={t.dangerDescription} size="sm" footer={<><Button variant="secondary" onClick={() => setConfirm(false)}>{t.cancel}</Button><Button variant="danger" onClick={() => { setConfirm(false); archive(); }}>{t.confirmArchive}</Button></>}><Badge tone={archived ? 'neutral' : 'positive'}>{archived ? t.archived : t.active}</Badge></Dialog>
    <Drawer open={drawer} onOpenChange={setDrawer} title={resource ? t.resources[resource.id] : name || t.drawerTitle} description={t.drawerDescription} footer={<Button variant="secondary" onClick={() => setDrawer(false)}>{t.cancel}</Button>}>
      <div className="drawer-preview"><Badge tone={!resource || resource.status === 'synced' ? 'positive' : 'neutral'}>{resource ? t[resource.status] : t.synced}</Badge><p>{notes || t.resourceDescription}</p><dl><div><dt>{t.owner}</dt><dd>{t.ownerValue}</dd></div><div><dt>{t.created}</dt><dd>2026-09-21</dd></div><div><dt>{t.location}</dt><dd>{t.locationValue}</dd></div>{resource && <div><dt>{t.resourceSize}</dt><dd>{resource.size} KB</dd></div>}</dl><Alert title={t.infoTitle}>{t.infoBody}</Alert><DropdownMenu label={t.menuLabel} trigger={<Button variant="secondary">{t.more}<MoreHorizontal /></Button>} items={[{ id: 'copy', label: t.duplicate, onSelect: () => { setDrawer(false); notify('copied'); } }]} /></div>
    </Drawer>
  </div>;
}
