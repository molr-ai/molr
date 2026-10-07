import { productMessages } from './i18n';
import { useEffect, useRef, useState } from 'react';
import { Terminal } from 'lucide-react';
import type { Locale } from './messages';
import { demoResources, statusLabels } from './interface-demo';
import type { InterfaceDemo } from './interface-demo';

type Entry = { command: string; output: string; error?: boolean };
export function InterfaceTerminal({ locale, demo }: { locale: Locale; demo: InterfaceDemo }) {
  const t = productMessages[locale];
  const [input, setInput] = useState('');
  const [entries, setEntries] = useState<Entry[]>([]);
  const [commands, setCommands] = useState<string[]>([]);
  const [cursor, setCursor] = useState(-1);
  const [draft, setDraft] = useState('');
  const [pending, setPending] = useState<{ kind: 'revoke'; resource: string } | { kind: 'create'; resource: string; title: string } | null>(null);
  const log = useRef<HTMLDivElement>(null);
  const resourceName = (id: string) => { const resource = demoResources.find(item => item.id === id); return resource ? locale === 'zh' ? resource.zh : resource.en : id; };
  const home = () => [`${demo.activeAssistant.name} / ${t.interfaceTerminal.ongoingConversation}`, '', `${t.common.tasks} ${demo.works.length}    ${t.common.resources} ${demoResources.length}    ${t.interfaceTerminal.connection} ${demo.connected ? 'online' : 'offline'}`, '', ...demo.works.map(work => `#${work.id}  ${statusLabels[locale][work.status]}  ${locale === 'zh' ? work.title : work.titleEn}`), '', t.interfaceTerminal.typeHelpForCommandsForHistoryTabToComplete].join('\n');
  const help = [
    'assistant list / use <id>      ' + t.interfaceTerminal.chooseAnAssistantConversation,
    'say <message>                  ' + t.interfaceTerminal.sendToCurrentConversation,
    'home                           ' + t.interfaceTerminal.assistantOverview,
    'task list                      ' + t.interfaceTerminal.listTasks,
    'task show <id>                 ' + t.interfaceTerminal.taskDetailsAndPlan,
    'task pause <id>                ' + t.common.pauseTask,
    'task resume <id>               ' + t.common.resumeTask,
    'task new <resource> <title>     ' + t.interfaceTerminal.createTaskReviewFirst,
    'task next <id>                 ' + t.interfaceTerminal.advanceSimulationIncludingVerification,
    'task budget <id> <amount>      ' + t.interfaceTerminal.updateTaskBudget,
    'task recover <id>              ' + t.interfaceTerminal.recoverCheckpoint,
    'task cancel <id>               ' + t.interfaceTerminal.cancelFutureExecution,
    'memory list                    ' + t.interfaceTerminal.listPersonalPreferences,
    'usage                          ' + t.interfaceTerminal.showTaskCosts,
    'resource list                  ' + t.interfaceTerminal.listResources,
    'resource show <id>             ' + t.interfaceTerminal.resourceDetailsAndFiles,
    'access list                    ' + t.interfaceTerminal.listAccess,
    'access allow <resource>        ' + t.interfaceTerminal.allowAccess,
    'access revoke <resource>       ' + t.interfaceTerminal.revokeAccessConfirmFirst,
    'connection                     ' + t.interfaceTerminal.connectionStatus,
    'connect / disconnect           ' + t.interfaceTerminal.simulateConnection,
    'clear / help                   ' + t.interfaceTerminal.clearHelp,
    '', t.interfaceTerminal.resourceIdsMeetingsDocumentsArchiveOptionalPrefixMolr,
    t.interfaceTerminal.exampleTaskNewMeetingsOrganizeMeetingsByMonth,
  ].join('\n');
  useEffect(() => { if (log.current) log.current.scrollTop = log.current.scrollHeight; }, [entries]);
  function append(command: string, output: string, error = false) { setEntries(previous => [...previous.slice(-79), { command, output, error }]); }
  function execute() {
    const raw = input.trim();
    if (!raw) return;
    setInput(''); setCursor(-1); setCommands(previous => [...previous, raw]);
    const args = raw.replace(/^molr\s+/, '').split(/\s+/);
    if (args[0] === 'work') args[0] = 'task';
    const [group, action, id] = args;
    const fail = (message: string) => append(raw, message, true);
    if (pending) {
      if (/^(n|no)$/i.test(raw)) { setPending(null); append(raw, t.interfaceTerminal.cancelledNothingChanged); return; }
      if (!/^(y|yes)$/i.test(raw)) { fail(t.interfaceTerminal.enterYToConfirmOrNToCancel); return; }
      if (!demo.connected) { setPending(null); fail(t.interfaceTerminal.disconnectedRunConnectFirst); return; }
      if (pending.kind === 'revoke') { demo.changeGrant(pending.resource, false); append(raw, t.interfaceTerminal.accessRevokedRelatedUnfinishedTasksIsBlocked); }
      else { const created = demo.createWork(pending.title, pending.resource); if (created) append(raw, `${t.interfaceTerminal.createdTask} #${created}\n→ task show ${created}`); else fail(t.interfaceTerminal.accessIsRequiredAllowTheResourceFirst); }
      setPending(null); return;
    }
    if (group === 'assistant' && action === 'list') { append(raw, demo.assistants.map(a => `${a.id}  ${a.name}`).join('\n')); return; }
    if (group === 'assistant' && action === 'use') { if (!demo.assistants.some(a => a.id === id)) { fail('Assistant not found'); return; } demo.selectAssistant(id); append(raw, `Assistant: ${id}`); return; }
    if (group === 'say') { const message = args.slice(1).join(' '); if (!demo.connected || !message) { fail('Connect first and provide a message.'); return; } demo.sendMessage(message, demo.activeAssistant.reference ?? undefined, locale); append(raw, t.interfaceTerminal.messageAddedToThisAssistantSOngoingConversation); return; }
    if (group === 'clear' && args.length === 1) { setEntries([]); return; }
    if (group === 'help' && args.length === 1) { append(raw, help); return; }
    if (group === 'home' && args.length === 1) { append(raw, home()); return; }
    if (group === 'connection' && args.length === 1) { append(raw, `MacBook / Local Runtime\n${demo.connected ? 'online' : 'offline'}\n${t.interfaceTerminal.disconnectingTheInterfaceDoesNotStopWork}`); return; }
    if (['connect', 'disconnect'].includes(group) && args.length === 1) { demo.setConnected(group === 'connect'); append(raw, group === 'connect' ? t.interfaceTerminal.connectedToLocalRuntime : t.interfaceTerminal.disconnectedCachedDataRemainsAvailable); return; }
    const mutating = (group === 'task' && ['new', 'pause', 'resume', 'next', 'recover', 'cancel', 'budget'].includes(action)) || (group === 'access' && ['allow', 'revoke'].includes(action));
    if (mutating && !demo.connected) { fail(t.interfaceTerminal.disconnectedRunConnectFirst); return; }
    if (group === 'usage') { append(raw, demo.works.map(item => `${item.id}  ¥${item.spent.toFixed(2)} / ¥${item.budget.toFixed(2)}  ${item.calls}/${item.maxCalls}`).join('\n')); return; }
    if (group === 'memory' && action === 'list') { append(raw, demo.memories.map(item => `${item.id}  ${item.text}`).join('\n') || t.common.noSavedPreferences); return; }
    if (group === 'task') {
      if (action === 'list' && args.length === 2) { append(raw, demo.works.map(work => `${work.id}  ${statusLabels[locale][work.status]}  ${locale === 'zh' ? work.title : work.titleEn}`).join('\n')); return; }
      if (action === 'new') {
        const title = args.slice(3).join(' ');
        if (!demoResources.some(item => item.id === id) || !title || title.length > 160) { fail('Usage: task new <meetings|documents|archive> <title (1–160)>'); return; }
        if (!demo.grants[id]) { fail(`${t.common.accessRequired}.\n→ access allow ${id}`); return; }
        setPending({ kind: 'create', resource: id, title });
        append(raw, `${t.interfaceTerminal.reviewPlan}\n${title}\n${resourceName(id)}\nBUDGET ¥${demo.defaultBudget.toFixed(2)} / ${demo.defaultMaxCalls} calls\n1. ${t.interfaceTerminal.readDirectoryAndIdentifyContent}\n2. ${t.interfaceTerminal.prepareOrganizationSuggestions}\n3. ${t.interfaceTerminal.saveResultsPreserveOriginals}\n\n${t.interfaceTerminal.startSampleTaskYN}`); return;
      }
      const work = demo.works.find(item => item.id === id);
      if (!['show', 'pause', 'resume', 'next', 'recover', 'cancel', 'budget'].includes(action) || args.length !== (action === 'budget' ? 4 : 3)) { fail('Usage: task list | show <id> | pause <id> | resume <id> | new <resource> <title>'); return; }
      if (!work) { fail(t.interfaceTerminal.taskNotFoundUseTaskListForAvailableIds); return; }
      if (action === 'budget') { const amount = Number(args[3]); if (!Number.isFinite(amount) || amount < Math.max(.2, work.spent) || amount > 100) { fail('Budget must cover spend and be within ¥0.20–100.'); return; } demo.setBudget(id, amount); append(raw, `${id} / ¥${amount.toFixed(2)}`); return; }
      if (action === 'next') { if (!['running', 'verifying'].includes(work.status)) { fail(t.interfaceTerminal.resolveTheWaitingConditionAndResumeFirst); return; } demo.advanceWork(id); append(raw, t.interfaceTerminal.simulationStepProcessedUseTaskShowToInspectThe); return; }
      if (action === 'recover') { if (work.status !== 'failed') { fail(t.interfaceTerminal.thisTaskDoesNotNeedRecovery); return; } demo.recoverWork(id); append(raw, t.interfaceTerminal.checkpointCheckedResumeManually); return; }
      if (action === 'cancel') { if (['complete', 'cancelled'].includes(work.status)) { fail(t.interfaceTerminal.taskHasEnded); return; } demo.cancelWork(id); append(raw, t.interfaceTerminal.futureExecutionCancelledExistingArtifactsPreserved); return; }
      if (action === 'show') { append(raw, `TASK ${work.id} / ${locale === 'zh' ? work.title : work.titleEn}\nSTATUS  ${statusLabels[locale][work.status]}\nSOURCE  ${resourceName(work.resource)}\nACCESS  ${demo.grants[work.resource] ? 'allowed' : 'blocked'}\nCOST    ¥${work.spent.toFixed(2)} / ¥${work.budget.toFixed(2)}\nCALLS   ${work.calls}/${work.maxCalls}\nVERIFIED ${work.verified}\nARTIFACT ${work.artifact ? 'available' : 'missing'}\n\n[${'━'.repeat(Math.round(work.done / work.total * 20))}${'─'.repeat(20 - Math.round(work.done / work.total * 20))}] ${Math.round(work.done / work.total * 100)}%\n${work.done} / ${work.total}\n\n${t.interfaceTerminal.planReadOrganizeSaveResultsPreserveOriginals}\n→ resource show ${work.resource}`); return; }
      if (['complete', 'cancelled', 'failed'].includes(work.status)) { fail(t.interfaceTerminal.taskIsCompletedItCannotBePausedOrResumed); return; }
      if (!demo.grants[work.resource]) { fail(`${t.interfaceTerminal.accessRequired}.\n→ access allow ${work.resource}`); return; }
      if ((action === 'pause' && work.status === 'paused') || (action === 'resume' && work.status === 'running')) { append(raw, statusLabels[locale][work.status]); return; }
      if (action === 'pause' && !['running', 'verifying'].includes(work.status)) { fail(t.interfaceTerminal.onlyRunningTasksCanBePaused); return; }
      if (action === 'resume' && work.stage < 3 && (work.spent + .2 > work.budget + .001 || work.calls >= work.maxCalls)) { fail(t.interfaceTerminal.budgetOrCallLimitReachedTaskNotResumed); return; }
      demo.toggleWork(id); append(raw, `${id} / ${action === 'pause' ? statusLabels[locale].paused : statusLabels[locale].running}`); return;
    }
    if (group === 'resource') {
      if (action === 'list' && args.length === 2) { append(raw, demoResources.map(item => `${item.id.padEnd(12)} ${resourceName(item.id)} / ${item.device} / ${item.count}`).join('\n')); return; }
      const resource = demoResources.find(item => item.id === id);
      if (action !== 'show' || args.length !== 3 || !resource) { fail('Usage: resource list | resource show <meetings|documents|archive>'); return; }
      append(raw, `${resourceName(id)}\n${resource.device} / ${resource.path}\n${demo.grants[id] ? resource.files.map(file => `  ${file}`).join('\n') : `${t.interfaceTerminal.accessRequiredToViewFiles}\n→ access allow ${id}`}`); return;
    }
    if (group === 'access') {
      if (action === 'list' && args.length === 2) { append(raw, demoResources.map(item => `${item.id.padEnd(12)} ${demo.grants[item.id] ? 'allowed' : 'blocked'}  ${item.path}`).join('\n')); return; }
      if (!['allow', 'revoke'].includes(action) || args.length !== 3 || !demoResources.some(item => item.id === id)) { fail('Usage: access list | access allow <resource> | access revoke <resource>'); return; }
      if (action === 'allow') { demo.changeGrant(id, true); append(raw, t.interfaceTerminal.accessAllowedResumeBlockedTasksManually); return; }
      setPending({ kind: 'revoke', resource: id }); append(raw, `${t.interfaceTerminal.revokingAccessBlocksRelatedTasksSavedResultsRemain}\n${resourceName(id)} — ${t.interfaceTerminal.revokeAccessYN}`); return;
    }
    fail(`${t.interfaceTerminal.unknownCommand}: ${raw}\n${t.interfaceTerminal.typeHelpForAvailableCommands}`);
  }
  return <div className="interface-preview ip-terminal-shell">
    <div className="ip-chrome"><Terminal size={15} /><span>molr — ~/home</span><span>CLI</span></div>
    <div className="ip-terminal-body">
      <div className="ip-terminal-heading">molr<span> / {t.interfaceTerminal.aLittleOrderForEverydayLife}</span></div>
      <div className="ip-terminal-log" ref={log} role="log" aria-label={t.interfaceTerminal.terminalOutput} tabIndex={0}>
        <pre className="ip-terminal-home">{home()}</pre>
        {entries.map((entry, index) => <div key={index} className={entry.error ? 'ip-terminal-error' : ''}><p className="ip-terminal-command">❯ {entry.command}</p><pre>{entry.output}</pre></div>)}
      </div>
      <form className="ip-terminal-input" onSubmit={event => { event.preventDefault(); execute(); }}>
        <label htmlFor="molr-cli-input">{pending ? '[y/n]' : '❯'}</label><input id="molr-cli-input" autoComplete="off" autoCapitalize="off" spellCheck={false} aria-label={t.interfaceTerminal.terminalCommand} placeholder={pending ? t.interfaceTerminal.yToConfirmNToCancel : t.interfaceTerminal.typeHelpThenPressEnter} value={input} onChange={event => setInput(event.target.value)} onKeyDown={event => {
          if (event.ctrlKey && event.key.toLowerCase() === 'l') { event.preventDefault(); setEntries([]); return; }
          if (event.ctrlKey && event.key.toLowerCase() === 'c') { event.preventDefault(); setPending(null); setInput(''); append('^C', t.interfaceTerminal.inputCancelled); return; }
          if (event.key === 'ArrowUp' || event.key === 'ArrowDown') { event.preventDefault(); if (!commands.length) return; if (cursor === -1) setDraft(input); const next = event.key === 'ArrowUp' ? cursor === -1 ? commands.length - 1 : Math.max(0, cursor - 1) : cursor === -1 || cursor === commands.length - 1 ? -1 : cursor + 1; setCursor(next); setInput(next === -1 ? draft : commands[next]); }
          if (event.key === 'Tab' && input && !pending) { const options = ['help', 'home', 'task list', 'task show ', 'task pause ', 'task resume ', 'task new ', 'resource list', 'resource show ', 'access list', 'access allow ', 'access revoke ', 'connection', 'connect', 'disconnect', 'clear']; const matches = options.filter(option => option.startsWith(input)); if (matches.length === 1) { event.preventDefault(); setInput(matches[0]); } }
        }} />
      </form>
      <p className="ip-terminal-hint">Enter ↵ · ↑ ↓ {t.interfaceTerminal.history} · Tab {t.interfaceTerminal.complete} · Ctrl+C {t.interfaceTerminal.cancel}<span>{demo.connected ? 'online' : 'offline'}</span></p>
    </div>
  </div>;
}
