import { useState, type ReactNode } from 'react';
import { AssistantAvatar, AssistantList, Badge, Button, ChatComposer, ChatMessage, ChatStatus, Logo, SearchField, Tabs, UIIcon, UIProvider, useTheme } from '@molr/ui';
import { Icon } from './HomeIcon';
import type { HomeCopy } from './home-content';
import type { Locale } from './site-content';
import './product-preview.css';

const noop = () => {};
const sceneIcons = ['chat', 'plan', 'folder'];

function PreviewHeader({ title, icon, children }: { title: string; icon: string; children?: ReactNode }) {
  return <header className="home-preview-header"><span className="home-preview-page-icon"><Icon name={icon} /></span><strong>{title}</strong>{children}</header>;
}

function PlanPreview({ copy }: { copy: HomeCopy }) {
  return <>
    <PreviewHeader title={copy.plans} icon="plan"><Button size="sm"><Icon name="plus" />{copy.newPlan}</Button></PreviewHeader>
    <div className="home-preview-tabs"><span className="selected">{copy.list}<small> · {copy.week}</small></span><span>{copy.calendar}</span></div>
    <div className="home-preview-body">
      <div className="home-plan-day">{copy.today}<span>3</span></div>
      {copy.planItems.map((item, index) => <div className="home-plan-item" key={item}>
        <span className={`home-task-dot ${index === 0 ? 'is-done' : ''}`}>{index === 0 && <Icon name="check" />}</span>
        <div><strong>{item}</strong><small>{copy.times[index]} · {index === 2 ? copy.personal : copy.assistant}</small></div>
        <Badge tone={index === 0 ? 'positive' : 'neutral'}>{copy.states[index]}</Badge>
      </div>)}
    </div>
  </>;
}

function ResourcesPreview({ copy }: { copy: HomeCopy }) {
  return <>
    <PreviewHeader title={copy.resources} icon="folder"><span className="home-preview-more">···</span></PreviewHeader>
    <Tabs className="home-preview-resource-tabs" label={copy.resources} value="model" onValueChange={noop} items={[
      { value: 'model', label: copy.modelType, icon: <Icon name="model" />, content: <div className="home-preview-body">
        {[{ name: 'OpenAI-compatible', provider: 'OpenAI' }, { name: copy.localModel, provider: 'Ollama' }].map(model => <div className="home-model-row" key={model.name}>
          <span className="home-preview-page-icon"><Icon name="model" /></span><div><strong>{model.name}</strong><small>{model.provider}</small></div><Badge tone="positive">{copy.connected}</Badge><span>···</span>
        </div>)}
      </div> },
      { value: 'tool', label: copy.tools, icon: <UIIcon name="tool" />, content: null },
      { value: 'sandbox', label: copy.sandbox, icon: <Icon name="server" />, content: null },
    ]} />
  </>;
}

function ChatPreview({ copy }: { copy: HomeCopy }) {
  return <>
    <header className="home-preview-chat-header">
      <UIIcon name="chevron-left" className="home-preview-back" /><strong>{copy.assistant}</strong><ChatStatus status="idle" compact live="off" />
      <div className="home-preview-chat-tools"><Icon name="folder" /><Icon name="plan" /><span>···</span></div>
    </header>
    <div className="home-preview-history">
      <ChatMessage role="user" sender={copy.personal} avatar={<AssistantAvatar name={copy.personal} />} text={copy.userMessage} readStatus="read" attachments={[{ id: 'notes', name: copy.attachment, size: 2400 }]} />
      <ChatMessage sender={copy.assistant} avatar={<AssistantAvatar name={copy.assistant} avatar="preset:robot" />} text={`${copy.reply}\n\n${copy.tasks.map((task, index) => `${index + 1}. ${task}`).join('\n')}\n\n${copy.replyEnd}`} />
    </div>
    <div className="home-preview-composer"><ChatComposer value="" onValueChange={noop} onSubmit={noop} onAttachFiles={noop} placeholder={copy.composer} /></div>
  </>;
}

/** Static public examples use the same presentation components as the official WebUI. */
export function ProductPreview({ copy, locale }: { copy: HomeCopy; locale: Locale }) {
  const [scene, setScene] = useState(0);
  const { theme } = useTheme();
  return <UIProvider locale={locale}><div className="home-product-block">
    <figure className="home-product-figure" role="img" aria-label={`${copy.previewLabel}: ${copy.scenes[scene]}`}>
      <div className="home-product" data-scene={scene} inert>
        <aside className="home-preview-nav">
          <Logo markOnly variant={theme === 'dark' ? 'light' : 'dark'} />
          {[copy.assistants, copy.plans, copy.resources].map((label, index) => <div className={`home-preview-nav-item${scene === index ? ' active' : ''}`} key={label}><Icon name={sceneIcons[index]} /><span>{label}</span></div>)}
          <div className="home-preview-account"><UIIcon name="search" /><span className="home-preview-nav-item"><Icon name="settings" /></span><span className="ui-assistant-avatar"><AssistantAvatar name={copy.personal} /></span></div>
        </aside>
        <div className="home-preview-workspace">
          {scene === 0 && <aside className="home-preview-assistants">
            <div className="home-preview-search"><SearchField label={copy.assistantSearch} hideLabel placeholder={copy.assistantSearch} value="" onValueChange={noop} /></div>
            <AssistantList label={copy.assistants} entries={[
              { id: 'everyday', name: copy.assistant, avatar: 'preset:robot', preview: copy.reply, pinned: true },
              { id: 'reading', name: copy.otherAssistant, avatar: 'preset:owl', preview: copy.assistantDetail },
            ]} selectedID="everyday" onSelect={noop} unreadLabel={String} pinnedLabel={copy.assistant} emptyLabel="" />
            <div className="home-preview-hire"><Button variant="secondary"><Icon name="plus" />{copy.newAssistant}</Button></div>
          </aside>}
          <div className="home-product-main">{scene === 0 ? <ChatPreview copy={copy} /> : scene === 1 ? <PlanPreview copy={copy} /> : <ResourcesPreview copy={copy} />}</div>
        </div>
      </div>
    </figure>
    <div className="home-scene-picker" role="group" aria-label={copy.scenesLabel}>
      {copy.scenes.map((label, index) => <button type="button" key={label} aria-pressed={scene === index} onClick={() => setScene(index)}><Icon name={sceneIcons[index]} />{label}</button>)}
    </div>
  </div></UIProvider>;
}
