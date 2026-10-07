import { useState } from 'react';
import { Button, ChatMessage, ChatMessageMeta, SegmentedControl, useUILocale } from '@molr/ui';
import { ComponentExample as Example } from './ComponentExample';

const samples = {
  zh: {
    title: 'Markdown / LaTeX 消息',
    description: '标题、列表、表格、代码与数学公式。点击消息底部的复制图标，保留原始文本中的全部格式标记。',
    sample: '示例内容', mixed: '混合内容', markdown: 'Markdown', math: 'LaTeX',
    source: '编辑原始文本', hint: '支持 $…$ 行内公式与 $$…$$ 独立公式。编辑后实时预览，也可以粘贴复制结果核对原文。',
    reset: '恢复示例', role: '消息角色', assistant: '助理', user: '用户',
    markdownText: '## 周末出行清单\n\n**轻松一点**，给自己留出 *自由安排* 的时间。\n\n- 沿湖散步，途中休息\n- 午后在附近用餐\n  - 下雨就去室内展馆\n\n> 不必赶路，按自己的节奏出发。\n\n| 时间 | 安排 | 预算 |\n| --- | --- | ---: |\n| 09:30 | 湖边散步 | ¥0 |\n| 12:00 | 午餐与咖啡 | ¥80 |\n\n- [x] 确认路线\n- [ ] 带上雨伞\n\n使用 `plan.duration` 查看时长，~~全天排满~~，留一点空白。\n\n```typescript\nconst plan = { pace: "slow", breaks: 2 };\nconsole.log(plan);\n```\n\n[查看 Markdown 语法](https://commonmark.org/help/)',
    mathText: String.raw`### 路程与速度

已知路程 $d = 6\,\mathrm{km}$，速度 $v = 4\,\mathrm{km/h}$，所需时间为：

$$
t = \frac{d}{v} = \frac{6}{4} = 1.5\,\mathrm{h}
$$

### 常用公式

$$
\begin{aligned}
(a+b)^2 &= a^2 + 2ab + b^2 \\
\int_0^1 x^2\,dx &= \frac{1}{3}
\end{aligned}
$$

矩阵也可以直接呈现：

$$
A = \begin{pmatrix}1 & 2 \\ 3 & 4\end{pmatrix},\qquad \det(A) = -2
$$`,
    mixedText: String.raw`## 一次轻松的步行计划

**建议预留 2 小时**，其中步行 90 分钟，休息 30 分钟。

- 路程：$d = 6\,\mathrm{km}$
- 速度：$v = 4\,\mathrm{km/h}$
- 休息：每 45 分钟停一次

> 留出弹性，比走完所有地点更重要。

总时长可以这样计算：

$$
T = \frac{d}{v} + t_{\mathrm{rest}} = \frac{6}{4} + 0.5 = 2\,\mathrm{h}
$$

| 阶段 | 时长 | 说明 |
| --- | ---: | --- |
| 步行 | 90 分钟 | 沿湖慢行 |
| 休息 | 30 分钟 | 咖啡与拍照 |

### 保存为代码

` + '```python\ndistance = 6\nspeed = 4\ntotal_hours = distance / speed + 0.5\n```' + '\n\n用 `total_hours` 随时调整计划。',
  },
  en: {
    title: 'Markdown / LaTeX messages',
    description: 'Headings, lists, tables, code, and math. The copy icon below each message preserves every formatting marker in the original text.',
    sample: 'Sample content', mixed: 'Mixed content', markdown: 'Markdown', math: 'LaTeX',
    source: 'Edit original text', hint: 'Use $…$ for inline math and $$…$$ for display math. Edit to preview live, or paste your copy to compare the source.',
    reset: 'Reset sample', role: 'Message role', assistant: 'Assistant', user: 'User',
    markdownText: '## Weekend checklist\n\n**Take it easy** and leave some *free time*.\n\n- Walk along the lake\n- Have lunch nearby\n  - Visit an indoor exhibition if it rains\n\n> Set your own pace. There is no rush.\n\n| Time | Activity | Budget |\n| --- | --- | ---: |\n| 09:30 | Lakeside walk | ¥0 |\n| 12:00 | Lunch and coffee | ¥80 |\n\n- [x] Confirm the route\n- [ ] Pack an umbrella\n\nCheck `plan.duration`; ~~fill every hour~~, leave some space.\n\n```typescript\nconst plan = { pace: "slow", breaks: 2 };\nconsole.log(plan);\n```\n\n[Markdown syntax guide](https://commonmark.org/help/)',
    mathText: String.raw`### Distance and speed

For a distance $d = 6\,\mathrm{km}$ and speed $v = 4\,\mathrm{km/h}$, walking time is:

$$
t = \frac{d}{v} = \frac{6}{4} = 1.5\,\mathrm{h}
$$

### Useful equations

$$
\begin{aligned}
(a+b)^2 &= a^2 + 2ab + b^2 \\
\int_0^1 x^2\,dx &= \frac{1}{3}
\end{aligned}
$$

Matrices work too:

$$
A = \begin{pmatrix}1 & 2 \\ 3 & 4\end{pmatrix},\qquad \det(A) = -2
$$`,
    mixedText: String.raw`## A relaxed walking plan

**Allow 2 hours**: 90 minutes of walking and 30 minutes of breaks.

- Distance: $d = 6\,\mathrm{km}$
- Speed: $v = 4\,\mathrm{km/h}$
- Breaks: stop every 45 minutes

> Leave room to pause and enjoy the view.

The total time is:

$$
T = \frac{d}{v} + t_{\mathrm{rest}} = \frac{6}{4} + 0.5 = 2\,\mathrm{h}
$$

| Stage | Duration | Notes |
| --- | ---: | --- |
| Walking | 90 min | Along the lake |
| Breaks | 30 min | Coffee and photos |

### Save it as code

` + '```python\ndistance = 6\nspeed = 4\ntotal_hours = distance / speed + 0.5\n```' + '\n\nAdjust `total_hours` whenever your plans change.',
  },
};

function RichTextContent({ locale }: { locale: 'zh' | 'en' }) {
  const t = samples[locale];
  const [sample, setSample] = useState<'mixed' | 'markdown' | 'math'>('mixed');
  const [role, setRole] = useState<'assistant' | 'user'>('assistant');
  const [text, setText] = useState(t.mixedText);
  const [feedback, setFeedback] = useState<'up' | 'down'>();
  const source = (value: typeof sample) => t[`${value}Text`];
  return <Example title={t.title} description={t.description} wide>
    <div className="chat-rich-text__controls">
      <SegmentedControl label={t.sample} value={sample} onValueChange={value => { const next = value as typeof sample; setSample(next); setText(source(next)); }} options={(['mixed', 'markdown', 'math'] as const).map(value => ({ value, label: t[value] }))} />
      <SegmentedControl label={t.role} value={role} onValueChange={value => setRole(value as typeof role)} options={[{ value: 'assistant', label: t.assistant }, { value: 'user', label: t.user }]} />
    </div>
    <div className="chat-rich-text__layout">
      <div className="chat-rich-text__preview"><ChatMessage role={role} sender={role === 'assistant' ? 'Molr' : t.user} text={text} footer={role === 'assistant' && <ChatMessageMeta showMetrics={false} feedback={feedback} onFeedback={value => setFeedback(current => current === value ? undefined : value)} />} /></div>
      <div className="chat-rich-text__source"><label htmlFor="chat-rich-text-source">{t.source}</label><textarea autoComplete="off" id="chat-rich-text-source" value={text} onChange={event => setText(event.target.value)} spellCheck={false} aria-describedby="chat-rich-text-hint" /><p id="chat-rich-text-hint">{t.hint}</p><Button size="sm" variant="secondary" onClick={() => setText(source(sample))}>{t.reset}</Button></div>
    </div>
  </Example>;
}

export function ChatRichTextExample() {
  const { locale } = useUILocale();
  return <RichTextContent key={locale} locale={locale} />;
}
