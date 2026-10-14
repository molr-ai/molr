# 组件库

`@molr/ui` 提供 Molr 的通用组件、组合组件、配色与品牌资源。组件使用 React 和 TypeScript，消费应用负责加载字体和页面基础样式。可操作示例位于 UI Design 的 `#components`，按输入、导航、反馈、弹层和数据展示分类。

## 接入

```tsx
import { UIProvider, ToastProvider, Button } from '@molr/ui';
import '@molr/ui/styles.css';

<UIProvider locale="en">
  <ToastProvider>
    <YourApp />
  </ToastProvider>
</UIProvider>
```

`UIProvider` 可通过 `brandAssetBase` 配置品牌资源目录（默认 `/brand`，嵌套 provider 默认继承）。`locale` 为 `zh`（默认）或 `en`，控制关闭、清除、分页、排序、选择等内置标签。业务文案通过组件属性传入；更新语言不会重建组件，也不会清空受控数据。需要在已有通知中切换语言时，可将响应语言状态的 React 节点传入通知文案。

主题由根元素的 `data-theme="light"` / `"dark"` 控制。可结合 `useTheme` 和 `ThemeSelect` 保存主题选择。所有组件使用同一套语义颜色变量；`ThemeSelect` 默认继承 `UIProvider` 的语言，也可单独传入 `locale`。装饰动画遵循 `prefers-reduced-motion`；Logo 的 `loading` 表达进行中的状态，保留动画，组件与下载的 SVG 行为一致。

`ThemeSelect` 默认使用工具栏的紧凑模式。设置表单可传入 `compact={false}`、`label` 和 `size="sm"`，显示完整标签与选项文字，移动端也不会收缩为图标。

## 视觉层级与边框

优先通过留白、底色、字号与选中状态表达层级。卡片、附件、提示块与浮层不使用装饰性描边；浮层沿用轻阴影表达悬浮关系。主要、强调、危险和文字按钮无边框；次要按钮（含图标按钮）保留 1 px 的 `--color-control-border` 细边框，使用 `--color-surface`（浅色主题为白色），与透明无边框的文字按钮区分。次要按钮悬停时使用 `--color-control-border-hover` 边框与 `--color-neutral-bg` 底色。

普通卡片（含资源和任务卡片）与表格主体使用 `--color-surface`，在明暗模式下均与页面背景分层。表头及行悬停使用 `--color-neutral-bg`，选中行保留独立的选中底色与标记；不在单元格上覆盖底色，以免遮住整行状态。平面列表行和嵌套正文区域按各自语义保持透明或凹入层级，不把所有列表都改成卡片。

`--color-border` 用于侧栏、页头、列表行、表格与日历等必要的区块分隔。输入框、下拉选择、复选框和单选框保留细边框，使用较淡的 `--color-control-border`；输入框与下拉选择悬停时使用 `--color-control-border-hover`。聚焦与错误状态继续使用清晰的语义色；键盘焦点指示、选中标记不因无边框设计而移除。

`Badge` 支持 `size="md" | "sm"`（默认 `md`）及 `dot`（默认 `true`）。紧凑编号使用 `<Badge size="sm" dot={false}>执行 01</Badge>`，小尺寸高度 22px，沿用组件的胶囊圆角和语义颜色。

普通标签使用专属的 `--color-badge-neutral` 与 `--color-badge-neutral-bg`，以略偏冷的中性灰底色区分卡片表面，并与成功绿、警告黄和错误红区分。普通优先级、无特殊状态的标记和数量标签共用此配色，保持无边框；悬停背景继续使用较轻的 `--color-neutral-bg`。

## 组件目录

| 类别 | 组件 | 支持的行为 |
| --- | --- | --- |
| 基础 | `Button`、`IconButton`、`Card`、`Badge`、`CodeBlock` | 五种按钮层级、三种尺寸、进行中与禁用、四种状态语义 |
| 输入 | `TextField`、`Textarea`、`SearchField` | 标签、原生表单属性、错误说明、多行输入、搜索清除 |
| 时间 | `DateTimePicker` | 1天、7天、1个月、永久与自定义日期时间；受控 ISO 值、最小时间、禁用态、sm/md/lg 尺寸 |
| 选择 | `Checkbox`、`RadioGroup`、`SegmentedControl`、`Switch`、`Select` | 多选及半选、单选、分段选项、开关、键盘单选下拉 |
| 导航 | `Tabs`、`Breadcrumb`、`Pagination` | 下划线或分段标签页、禁用项、路径、页码与边界 |
| 弹层 | `Dialog`、`Drawer`、`DropdownMenu`、`Popover`、`Tooltip` | 模态任务、侧边详情、动作列表、非模态详情、悬停或聚焦说明 |
| 反馈 | `Alert`、`ConnectionNotice`、`ToastProvider` / `useToast`、`EmptyState`、`Skeleton`、`Progress` | 页面内消息、可撤销通知、空白与错误状态、占位和进度 |
| 数据 | `Table` | 表格语义、受控排序、当前页全选及跨页选择、加载与空状态 |
| 组合 | `ResourceCard`、`TaskCard` | 资源状态与操作、任务进度与继续操作 |
| 聊天 | `ChatBubble`、`ChatMessage`、`ChatComposer`、`ChatCard`、`ChatAttachmentList`、`ChatMessageMeta`、`ChatStatus` | 文本气泡、头像与消息组合、自适应输入、独立卡片、附件、信息与反馈、助理状态 |
| 品牌与设置 | `Logo`、`ThemeSelect` | 动态品牌标识、昼夜编排、明暗主题选择 |

## 基础与输入

`Button` 的 `variant` 为 `primary`、`secondary`、`ghost`、`accent` 或 `danger`，`size` 为 `sm`、`md`、`lg`。`loading` 显示进度标记，同时禁用按钮并设置 `aria-busy`；保留可读的按钮内容。`IconButton` 必须提供 `label`。

可切换图标按钮使用 `aria-pressed` 表达选中态：`.ui-icon-button` 未选中时为线框，默认选中后取消描边并使用纯填充，通过 `--icon-selected-color` 指定填充色（默认 `currentColor`）。聊天赞／踩由独立的 `ChatFeedback` 实现，保持原版轮廓与线宽，选中时袖套实色、手掌浅色填充；赞使用 `--color-feedback-like`，踩使用 `--color-feedback-dislike`。再次点击取消，赞和踩互斥，无选中底色，保留键盘焦点；保存中的反馈保留焦点并通过 `aria-disabled` 阻止重复提交。`aria-expanded` 弹层触发器与 `aria-current` 导航状态保留各自的语义。

`TextField` 与 `Textarea` 必须提供 `label`，可传入 `description`、`error`，错误优先于说明。它们接受相应原生输入属性，支持 `name`、`required`、`disabled`、`readOnly`、`maxLength` 等。

```tsx
<Textarea label="项目说明" value={notes} onChange={e => setNotes(e.target.value)} />
<SearchField label="搜索资源" value={query} onValueChange={setQuery} hideLabel />
<Checkbox label="选择全部" checked={allSelected} indeterminate={partlySelected}
  onChange={e => selectAll(e.target.checked)} />
```

`SearchField` 是受控的原生 `search` 输入；清除后保留输入焦点，禁用与只读状态不显示清除按钮。`trailingAction` 可在输入框最右侧放置筛选等图标操作，清除按钮位于该操作左边；左右留白为 12 px，组件自动为图标与两侧操作预留文字空间。它只处理关键词，筛选数据由调用方负责。

`Checkbox` 的 `indeterminate` 是受控半选态；父组件接受点击时，应在 `onChange` 中同步更新它，或通过 `onIndeterminateChange` 接收清除半选的请求。只需要初始半选且点击后自动取消时，用 `defaultIndeterminate`。父组件持续传入 `indeterminate={true}` 时组件保持半选，不因 checked 改变而自行覆盖业务状态。图标由原生 `:indeterminate` 状态决定，与表单和辅助技术一致。另支持 `description`、`error` 和 `hideLabel`。`RadioGroup` 与 `SegmentedControl` 共用 `{ label, value, onValueChange, options, name?, disabled?, description?, error?, hideLabel? }` 接口；选项可带图标、说明和禁用标记。分段控件适合简短选项。两者使用原生 radio，方向键切换及表单序列化由浏览器处理。

`Switch` 支持 `size="sm" | "md" | "lg"`：轨道分别为 30×18、36×22、44×26 px；默认 `md` 保持原尺寸，设置页使用 `lg`。`Select` 的 `sm` 同时缩小触发器与菜单：选项字号 12 px、最小高度 36 px、内边距 8×10 px、选项间距 4 px，搜索框与图标同步缩小。既有接口保持兼容；详见 [UI 包说明](README.md#下拉选择)。

`Select` 与输入框共享弱边框、表面色及向内加粗的聚焦效果；展开状态沿用同一焦点边框，错误色优先。下拉选项以选中底色、字重与勾选标记区分状态；hover、键盘活动态与选中态共用底色，选项间隔 4 px。`multiple` 模式接收数组值，点击、Enter 或空格切换选中并保持展开；Tab 和 Escape 关闭菜单。 可选 `searchable`（默认 `false`，由调用方配置）在弹层顶部显示搜索框，按 `label`、`value`、`description` 和分组名称忽略大小写匹配关键词；`searchPlaceholder` 可覆盖占位文案。搜索只过滤展示，不改变已有选择，支持单选和多选；打开自动聚焦搜索，方向键切换结果、Enter 选择、Escape 取消并回到触发器、Tab 关闭并继续焦点导航。搜索框保留空格与 Home/End 的文本编辑行为，关闭后重新打开会清空搜索；无结果显示本地化提示。选项可传入 `group: { value, label }`，按分组标识聚合展示并提供具名 `group` 语义；组头不可选，键盘跳过组头，筛选后不显示空组。`trigger` 可传入图标按钮，仍保留同一套列表、键盘行为与多选语义。筛选、表单、聊天模型及主题切换统一复用 `Select`，不混用浏览器原生下拉框。

## 导航

```tsx
<Tabs label="项目详情" value={tab} onValueChange={setTab} items={[
  { value: 'overview', label: '概览', content: <Overview /> },
  { value: 'activity', label: '活动', content: <Activity /> },
]} />
<Breadcrumb items={[{ label: '项目', href: '/projects' }, { label: '品牌探索' }]} />
<Pagination page={page} totalPages={totalPages} onPageChange={setPage} />
```

`Tabs` 支持 `variant="line" | "segmented"`、`size="sm" | "md"` 和 `orientation="horizontal" | "vertical"`，默认使用 `line`、`md`。原 `pill` 命名统一为 `segmented`。方向键循环切换可用项，Home / End 到首末项；只有选中项进入 Tab 顺序。非活动面板保留挂载并通过 `hidden` 隐藏，内部状态得以保留。适用于可以立即呈现的本地内容。

切换控件仅使用以下两套视觉，`Tabs` 的分段变体与 `SegmentedControl` 共享样式：

| 样式 | 视觉 | 用法 |
| --- | --- | --- |
| 下划线简约版 `line` | 透明背景、2 px 选中下划线、24 px 项间距 | 组件分类、Web / Desktop / Mobile / CLI、资源 / 授权、日程分类 |
| 带底色分段控件 `segmented` | `--color-background` 底色、选中项使用 `--color-surface` 与轻阴影；外圆角 8 px、内圆角 4 px、内边距与项间距 4 px | 列表 / 月历、Logo 动画选项、资源筛选、表单中的展示方式 |

默认 `md` 尺寸下，两种样式共用 14 px 字号、图标尺寸、选中文字、禁用状态与可见焦点。单项最小高度统一为 36 px，触屏或窄屏下不再额外放大。横向 Tab 与切换控件在非移动端超宽时换行；`mobile` 断点（700 px）及以下的窄屏保持单行并横向滚动。Mobile 原型通过 `--choice-wrap: nowrap` 在任意预览宽度下沿用移动端滚动规则，竖向 Tab 保持纵向排列。

`Tabs size="sm"` 使用 12 px 字号、14 px 图标和 28 px 单项最小高度；分段样式的容器内边距与项间距均为 3 px，适合详情面板内的次级切换。

移动端隐藏滚动条但保留触摸滑动。选中首项或末项时滚到开头或末尾；中间项未完整显示时，滚动到完整可见，并尽量露出滚动方向一侧相邻项的 12 px。自动定位使用平滑滚动，系统开启减少动态效果时直接定位。中间项已完整可见时不调整位置；再次点击当前项、容器尺寸变化或隐藏面板重新显示时也会核对位置。`Tabs` 与 `SegmentedControl` 内置此行为，使用原生按钮组合的 `ui-choice-list` 可将 `useChoiceScroll(当前值)` 返回的 ref 绑定到列表，复用相同规则。

需要保留按钮语义及再次点击行为的组合控件，使用 `ui-choice-list ui-choice-list--line` 或 `ui-choice-list ui-choice-list--segmented` 容器，直接子项为按钮，以 `aria-pressed`（选项）或 `aria-current="page"`（当前位置）表达选中状态，并为组提供名称。它们复用相同的底层样式；页面样式只负责布局，不另设圆角、选中色或字号。内容面板仍优先使用 `Tabs`，表单单选使用 `SegmentedControl`。

`Breadcrumb` 的末项为当前位置，其余有 `href` 的条目为链接。`Pagination` 使用从 1 开始的页码；首尾按钮在边界禁用，页数较多时省略中间页码，手机显示当前页与前后按钮。数据获取、排序与切页由调用方负责。

## 聊天（Chat）

助理消息气泡、`ChatCard`、文件附件、图片附件缩略图和用户默认头像使用 `--color-background`，在 `--color-surface` 聊天区域中形成层次；用户气泡使用 `--color-accent` 与 `--color-on-accent`，输入器保持 `--color-surface`。这些样式由共享组件提供，UI Design 与 WebUI 直接复用。

`ChatBubble` 只接收 `text` 字符串，双方气泡均无边框、16 px 圆角；`role="user"` 使用 Lime 底色与 Forest 文字。用户气泡内的文本选中使用 Forest 底色与 Canvas 文字，避免与气泡底色重合。`ChatMessage` 组合头像、`sender`、文本、`attachments`、`images`、`cards` 和 `footer`，头像、图片、附件和卡片独立于文本气泡呈现。默认显示 32 × 32 px 圆形头像，未提供 `avatar` 时以发送者名称首字占位；可传入图片或图标。`showAvatar` 与 `showSender` 可独立开关，助理头像在左侧，用户头像在右侧。用户消息可传入 `readStatus="unread"` 或 `readStatus="read"`，在气泡和附件下方右对齐显示「未读／已读」，使用辅助文字色，不增加边框。未传值时不显示；助理消息不显示此状态，状态由调用方提供。

```tsx
<ChatMessage role="assistant" sender="Molr" text="已整理好出行资料。"
  cards={<ChatCard title="周末清单" eyebrow="交付物 · v1" status={{ label: '已就绪', tone: 'positive' }} />}
  footer={<ChatMessageMeta metrics={{ inputTokens: 128, outputTokens: 86, firstTokenMs: 320, tokensPerSecond: 42.6 }}
    feedback={feedback} onFeedback={value => setFeedback(current => current === value ? undefined : value)} />} />
<ChatComposer value={draft} onValueChange={setDraft} onSubmit={send}
  attachments={files} onAttachFiles={attach} onRemoveAttachment={remove}
  models={[{ value: 'standard', label: '标准' }]} model={model} onModelChange={setModel}
  onVoiceInput={toggleRecording} recording={recording} sending={sending} onStop={stop} />
```

`ChatComposer` 使用受控草稿，textarea 从 28 px 自适应到 180 px，超过后内部滚动；外框默认使用弱边框，悬停时轻微加深，聚焦时向内加粗。点击空白区域自动聚焦 textarea，按钮、文件输入和模型选择保留自身事件；禁用时不响应空白区域点击。Enter 发送、Shift+Enter 换行，输入法组合期间不发送。左下附件按钮通过 `onAttachFiles` 返回 `File[]`，右下为模型、语音和发送／停止按钮；`disabled` 禁用所有输入，`sendDisabled` 只限制提交，`sending` 配合 `onStop` 展示停止生成。`hint` 和 `notice` 分别提供辅助说明和状态反馈。录音、文件限制、上传、回复生成与持久化均由调用方负责。

`lock={{ title, description?, action? }}` 展示通用锁定样式，保留输入框的轮廓、表面色和圆角，以锁图标、标题、说明和可选操作替代编辑区与工具栏。锁定时不显示草稿、附件、输入提示或发送控件，操作区保持可用；移除 `lock` 后恢复调用方持有的草稿和附件。`description` 与 `action` 支持 ReactNode，组件不包含渠道业务；微信聊天入口通过此配置展示发送方式、解绑恢复输入的说明与管理按钮。

`ChatAttachmentList` 显示文件名和大小，传入 `onRemove` 可移除草稿附件。`ChatCard` 接收标题、状态、说明、`details` 信息行、内容、元信息和卡片内操作，16 px 圆角，与文本气泡分开。卡片最大宽度为 480 px，包含卡片的消息容器也按此宽度适配，窄屏随可用空间收缩。`details` 为 `ChatCardDetail[]`（`{ label: ReactNode; value: ReactNode }`），统一渲染为语义化 `dl / dt / dd`；任务、交付物和权限申请共享同一套标题、信息行与操作区，不另建权限专属卡片外观。`ChatMessageMeta` 将 token 用量、首 token 延迟、解码速度和模型收进信息图标，缺失数据展示「—」；赞／踩状态由调用方控制，也可单独使用 `ChatFeedback`（`value?: 'up' | 'down'`、`onChange`、可选 `pending?: 'up' | 'down'`）。`pending` 指定正在保存的按钮，保留手势并在 300 ms 后显示小圆点，同时阻止重复操作；`ChatMessageMeta.feedbackPending` 透传此状态。调用方负责乐观选择、请求和失败回滚。保留原版图形，选中时袖套实色填满、手掌使用同色 18% 浅填充，无按钮边框或底色；赞为红色、踩为灰色。图标为 15 px，与复制／信息图标使用相同线宽，按钮间距为 8 px。用户激活时点赞轻弹、点踩下压回弹，取消或加载已保存状态不播放；遵循系统与 `data-reduced-motion="true"` 偏好。`ChatStatus` 是独立的助理状态组件，通过 `status` 支持 `acting`（行动）、`thinking`（思考）、`typing`（输入中）、`waiting`（等待）、`complete`（完成）、`error`（异常）和 `idle`（空闲）。空闲状态的圆点使用 Lime，状态文字沿用辅助文字色。`label` 可替换状态名称，`description` 补充当前动作，例如「正在浏览文件…」；`compact` 用于对话顶部。图标与文字共同表达状态，只有进行中的状态有轻量动效，尊重减少动态效果设置；默认使用礼貌播报，同一状态多处展示时可用 `live="off"` 避免重复播报。`ChatTypingIndicator` 保留为输入中状态的兼容封装。所有内置标签跟随 `UIProvider`。

UI Design 的 聊天组（英文版为 Chat）提供独立的本地演示：发送、追加、逐步输出、停止、附件移除、语音状态、反馈和卡片详情／下载。示例录音不访问麦克风，示例测量数据明确标注，不写入助理原型的数据。产品原型的消息、输入框、任务卡片和对话顶部状态复用上述组件。用户消息发送后为未读，对应的模拟回复到达后改为已读，状态按消息及助理隔离并保存在本地；历史消息根据已存在的对应回复补齐状态。独立流式对话示例在首段回复出现时标记已读，仅发送示例保留未读。消息头像默认开启，可在设置 → 界面中切换，与发送者名称设置独立；已有原型数据自动补齐默认值。

## 弹窗与抽屉

```tsx
<Dialog open={open} onOpenChange={setOpen} title="编辑项目" description="更新名称与说明。"
  footer={<Button form="project-form" type="submit">保存</Button>}>
  <form id="project-form" onSubmit={saveProject}>…</form>
</Dialog>
```

`Dialog` 与 `Drawer` 使用相同接口：`open`、`onOpenChange`、`onAfterClose?`、`title`、`description?`、`children?`、`footer?`、`headerContent?`、`closeLabel?`、`size?`、`initialFocusRef?`、`dismissible?`、`showCloseButton?`。`size` 为 `sm`、`md` 或 `lg`；未提供正文时不渲染 body，只有标题／说明和操作按钮的确认弹窗不会保留空白正文区，相邻 header/footer 只保留一条分隔线。标准 Dialog 在视口内居中，最大高度为 `90dvh`，正文区域独立滚动，页面的子元素间距不应影响其定位。抽屉在窄屏占满宽度，容器内抽屉仍限制在预览范围内。`headerContent` 可将搜索框等控件放到关闭按钮左侧，标题和说明仍保留为读屏标签。`showCloseButton={false}` 仅隐藏关闭按钮，保留 Escape 和遮罩点击关闭。

默认抽屉以直角贴齐视口右侧，底部抽屉使用顶部圆角；打开时用 280 ms 滑入，关闭时用 220 ms 滑出，遮罩同步淡入淡出。退出完成后才解除模态状态、恢复页面滚动和触发按钮焦点；中途重新打开会从当前位置衔接。通过 `open` 控制显隐时，组件保留退出开始前的标题、正文和底部操作区，退出结束后更新为关闭状态的内容。条件渲染的编辑器统一写作 `<ModalPresence>{selection && <Editor … />}</ModalPresence>`：边界保留整个表单子树，向后代 Dialog / Drawer 传递关闭状态，等所有嵌套弹层退出后再卸载、清理订阅和草稿；边界自身必须保持挂载。异步数据会切换加载弹窗和编辑器根节点时，用 `useModalSnapshot(data)` 保留关闭时的数据分支，避免加载完成打断退出。退出期间阻止点击、键盘输入和提交，快速重开保留当前草稿与动画位置。`onAfterClose` 在退出结束且弹层关闭后触发一次。页面导航或边界整体卸载时立即清理；系统或应用开启减少动态效果时直接切换状态。

模态层使用原生 `<dialog>` 进入顶层，背景不可操作并停止页面滚动。默认将焦点放在标题，可指定初始焦点；Tab 在内部循环，关闭后恢复触发元素焦点。Escape、关闭按钮及从遮罩开始和结束的点击可以关闭；从内容拖到遮罩不会误关。`dismissible={false}` 暂停这些关闭入口，适合短暂的提交状态，调用方仍负责结束状态或提供取消操作。

`ModalScope` 通过 `container` 指定一个定位且裁切溢出的预览容器。范围内的 Dialog、Drawer 与 Popover 使用容器内的底部抽屉与遮罩，不锁定整个网页；内容通过 portal 渲染到容器根部，不受内部聊天滑动或滚动影响。打开时只将范围内的背景与下层抽屉设为 inert，关闭完成后解除并恢复焦点；下拉菜单和 Select 的浮层也按该容器边界定位。

嵌套模态层的 Escape 和遮罩点击只处理当前层。已经关闭的弹窗卸载时不会再次改变焦点。

## 菜单、详情和提示

```tsx
<DropdownMenu label="项目操作" trigger={<Button>更多操作</Button>} items={[
  { id: 'rename', label: '重命名', onSelect: rename },
  { id: 'archive', label: '归档', danger: true, separatorBefore: true, onSelect: archive },
]} />
<Popover title="共享设置" trigger={<Button>共享</Button>}><SharingSettings /></Popover>
<Tooltip content="查看项目帮助"><IconButton label="帮助"><HelpIcon /></IconButton></Tooltip>
```

触发元素应为一个可聚焦的按钮，或把 ref、事件及 ARIA 属性传到原生按钮的组件；`Button`、`IconButton` 可直接使用。不要给触发元素再套按钮。`DropdownMenu` 和 `Popover` 支持 `align="start" | "end"`。菜单项传入 `selected` 时使用单选菜单语义，选中项显示勾选标记；普通动作项无需传入。

触发元素可以使用对象 ref 或回调 ref，包括返回清理函数的回调。稳定的回调不会因为弹层状态更新而反复解绑；卸载时执行相应清理。

菜单项接受 `id`、`label`、`onSelect`、`icon?`、`shortcut?`、`disabled?`、`danger?`、`separatorBefore?`。方向键、Home / End、字符前缀查找可定位选项；Enter / 空格执行，Escape 取消并恢复焦点。`shortcut` 仅显示快捷键说明，快捷键绑定由调用方负责。

`Popover` 是有标题的非模态详情容器，适合放置可操作内容；可选 `open` / `onOpenChange` 进行外部控制。`Tooltip` 仅用于补充说明，`content` 接受文本或 ReactNode，支持通过语义标记展示多行提示，默认延迟 400 ms，在悬停或聚焦时显示，可用 Escape 关闭。`side` 可选 `top`（默认）、`bottom`、`left` 或 `right`，空间不足时翻转；`width` 默认 220，也可设为数字或 `auto` 随文字收紧。

Tooltip 在触发元素仍有焦点时，不会因为鼠标移开而消失；鼠标也可以移入提示内容。焦点位于弹窗内的提示按钮时，第一次 Escape 关闭提示，下一次才关闭弹窗。

窗口失焦或页面隐藏时，Tooltip 关闭并取消延迟显示。切回页面恢复原按钮焦点时不自动显示；用户再次通过键盘导航聚焦或移动鼠标悬停后恢复正常提示，不改变按钮本身的焦点。

Dialog / Drawer 关闭并恢复触发按钮焦点时，同样不会自动弹出 Tooltip；重新通过键盘导航聚焦或移动鼠标悬停后再显示。

弹层通过原生 Popover API 进入顶层，按视口空间选择上下方向和宽高，可嵌入模态层。需要支持 `<dialog>`、Popover API 与 CSS `:has()` 的现代浏览器。

自研 contained 浮层可从包入口导入 `useModalScope`、`getOverlayViewport`、`useFloating`、`composeRefs` 与 `useComposedRefs`，对应类型包括 `ModalScopeValue`、`FloatingOptions`、`FloatingSide` 和 `FloatingTrigger`。`useFloating(open, onOpenChange, options)` 返回 `anchorRef`、`panelRef`、`style`、`side`，自动读取所在 ModalScope 的边界；`maxHeight` 可指定浮层高度上限（默认 400px），实际仍受视口可用空间约束；将 style 与 panelRef 传给浮层。Popover 的 `auto` / `manual` 模式由 JSX 的 `popover` 属性明确决定，不是定位 hook 的参数。使用 auto 模式时，调用方还需通过 toggle 事件同步关闭状态。`getOverlayViewport` 在服务端返回零尺寸，实际定位在客户端完成。

## 反馈

```tsx
const { toast, dismiss, update } = useToast();
toast({ title: '保存完成', tone: 'positive' });
toast({ title: '项目已归档', action: { label: '撤销', onClick: undoArchive } });

<Alert title="同步未完成" tone="danger" action={<Button onClick={retry}>重试</Button>}>
  检查连接后可以继续。
</Alert>
<EmptyState title="还没有项目" description="从一个小小的好奇开始。" action={<Button>新建项目</Button>} />
<Progress label="同步资源" value={42} />
<Progress label="正在查找内容" />
```

`ConnectionNotice` 接收 `connection: connected | reconnecting | disconnected | failed | recovered`、`onRetry` 和可选 `className`。重连显示旋转图标，断开和失败显示重试按钮，已连接及恢复状态不渲染提示；连接状态、超时和重试结果由调用方提供。组件与按钮统一使用 12px 字号，文字、图标及操作背景沿用警示或错误语义色，默认标签跟随 `UIProvider` 的语言，减少动态效果遵循 `UIProvider`、系统和 `data-reduced-motion` 设置。UI Design 的 `?components=feedback#connection-notices` 提供状态切换及成功／失败交互预览。

`Alert` 和 Toast 支持 `neutral`、`positive`、`warning`、`danger`。错误使用 alert 语义，其余通知使用 status；信息同时通过文字与图形表达。`Alert` 可传入 `onDismiss` 和 `dismissLabel`。

`toast()` 返回通知 ID，可通过 `dismiss(id)` 关闭或 `update(id, options)` 更新。普通通知默认 5 秒消失；带操作的通知默认常驻，可显式设置 `duration`，`0` 表示常驻。悬停、键盘焦点位于通知内、标签页隐藏时暂停计时，恢复后继续剩余时间。通知包含可读的关闭按钮。`action.disabled` 可在重试冷却期间禁用操作；`onDismiss` 在手动关闭、点击操作或自动消失时调用，程序调用 `dismiss(id)` 不触发它。通知区域以 manual popover 进入 top-layer，并在组件库 Dialog / Drawer 切换时移动到当前可交互的弹层内部；移动同一容器保留计时和焦点，不重新挂载通知。后打开的菜单不会盖住已有通知。`useToast` 位于 `ModalScope` 内时，通知限制在相应预览范围内；该范围不可见时暂停计时。Toast Context 的方法和 value 保持稳定，通知增删不会广播给所有调用方。

`EmptyState` 支持 `illustration` 和 `action`，图形为装饰内容。`Skeleton` 提供 `line`、`circle`、`block` 三种形状以及宽高；由外层容器提供加载说明和 `aria-busy`。浅色与深色使用独立的中性灰 token `--color-skeleton-base` / `--color-skeleton-highlight`，减少动态效果时停止流光。UI Design 的「骨架图」分类展示基础形态及列表、卡片的加载前后状态，与 WebUI 复用同一组件和配色。`Progress` 不传 `value` 时为不确定进度，传入时按 `max`（默认 100）计算并约束数值，可用 `valueText` 提供更具体的描述。

## 数据与组合

```tsx
<Table caption="项目资源" rows={pageRows} rowKey={row => row.id}
  columns={[
    { key: 'name', header: '名称', cell: row => row.name, sortable: true },
    { key: 'size', header: '大小', cell: row => row.size },
  ]}
  sort={sort} onSortChange={setSort}
  selection={{ keys: selectedKeys, onChange: setSelectedKeys, rowLabel: row => row.name }}
  emptyState={<EmptyState title="没有找到结果" />}
/>
```

`Table` 是语义化表格，列接受 `key`、`header`、`cell`、`sortable?`、`align?`。排序状态为 `{ column, direction: 'asc' | 'desc' }`，组件发出排序意图并显示 `aria-sort`，调用方应在分页之前对完整集合排序。每页全选只改变当前页可选行，保留其他页的选择；可用 `selection.isDisabled` 排除行。`loading` 展示加载行，`emptyState` 展示空结果。窄屏只在表格区域内横向滚动，该区域可通过键盘聚焦。

`ResourceCard` 接收 `name`、`description?`、`icon?`、`status?: { label, tone }`、`metadata?`、`action?`。`TaskCard` 接收 `title`、`description?`、`status`、`progress?: ProgressProps`、`metadata?`、`actions?`。组合卡片不绑定数据来源、后台任务或权限逻辑，状态与操作由调用方提供。

## 验证

在根目录执行：

```bash
npm run typecheck
npm run build
npx playwright install chromium    # 首次安装测试浏览器
npm run test:ui
```

已有兼容 Chromium 时，可设置 `PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH` 指定可执行文件。测试自动启动监听 `0.0.0.0:5188` 的开发服务，本地已有服务则复用。

浏览器用例覆盖表单校验、半选、禁用项、标签页键盘、分页边界、弹层焦点与关闭、菜单内触发弹窗、抽屉内菜单、通知撤销及暂停计时、加载与进度、表格搜索与跨页选择、中英文切换、明暗主题、手机布局及减少动态效果。测试失败保留截图与 trace，产物不纳入版本控制。

`Drawer` 额外接受 `side="right" | "bottom"`，默认从右侧进入；bottom 从底部进入。两种方向共用模态焦点、Escape、点击外部关闭与减少动态效果行为，调用方负责根据视口选择方向。

## 平台行为约定

- `useTheme` 的所有调用方共享一个外部 store、一组系统主题及 storage 监听，以及一个 documentElement 写入入口；最后一个调用方卸载时释放监听。浏览器存储不可用时仍保持内存中的选择。SSR 返回稳定的 system/light 快照，客户端接入后同步实际设置。
- Toast 的 document 访问和 portal 容器创建都在客户端进行，服务端渲染不需要浏览器全局对象。
- Select 是 select-only combobox：关闭态 ArrowUp 打开到第一个可用项，End 打开到最后一项；这是 WAI-ARIA 对应示例的约定。DropdownMenu 是菜单模式，ArrowUp 打开到最后一个动作。两者 typeahead 使用与宿主语言无关的 toLowerCase，避免英文标签被系统区域设置改变。
- Dialog 与 Toast 共用焦点候选计算，包括带 controls 的 audio/video、首个 summary 和可编辑区域；隐藏、inert、禁用、关闭 details 的内部内容及 tabindex=-1 均不纳入 Tab 顺序。
- 选中的赞/踩及录音按钮统一使用填充色，取消额外描边；未选中图标保留线条。
- 暗色主题显式保留 accent-hover、danger-fill、danger-fill-hover、notification 的实色值；它们仍搭配森林色或白字。文字、柔和背景及反馈图标颜色继续按主题适配，不将共享实色误判为漏配。

键盘约定参考 [WAI-ARIA Select-only Combobox](https://www.w3.org/WAI/ARIA/apg/patterns/combobox/examples/combobox-select-only/)。

响应式规则使用共享 `breakpoints.json`。CSS 通过 `@molr/ui/postcss-breakpoints` 编译命名媒体查询；组件使用 `useBreakpoint`，不再在 `matchMedia` 中复制宽度。接入配置见 [README](README.md)。

## 助理列表与应用动效偏好

`SlidingHighlight` 是共用背景滑块容器，助理列表与桌面用户设置、管理员设置侧栏均复用它。`as` 支持 `div`（默认）、`nav`、`ul`；`value` 对应子项的 `data-sliding-item`，`disabled` 关闭滑块而不禁用子项操作。容器保留调用方的列表／导航语义，点击、键盘操作和 `aria-current` 由调用方维护。默认颜色为 `--color-selected-bg`，可通过 `--sliding-highlight-background` 覆盖；使用 `ResizeObserver` 跟随条目尺寸及列表布局变化。

桌面侧栏使用单一背景滑块：悬停项优先于当前选中项，移出后返回选中项；两者均不存在时隐藏。从隐藏变为显示时直接定位，已显示时平滑移动；系统或应用要求减少动效时直接切换。移动端布局及手机宽度下始终隐藏背景滑块，包括鼠标悬停和触摸操作。

`AssistantList` 接受纯展示数据 `entries: AssistantListEntry[]`、`selectedID`、`onSelect` 和读屏标签。条目包含 `id/name/preview/unread/pinned`，组件不读取 demo store 或发起网络请求。`mobile` 控制移动端布局和选中表现，列表平铺展示、置顶优先；间距、头像、未读标记统一由组件样式提供。原型和真实 WebUI 共用这一实现，不提供助理分组展示。可传入 `emptyState` 复用 `EmptyState` 展示图标、说明和操作；未提供时回退到 `emptyLabel`。应用区分无助理的聘请引导和搜索无结果的清空操作。

`SearchPanel` 同样由原型和 WebUI 共同使用。调用方提供受控 `query/scopes`、数据源 `sources`、快捷入口 `shortcuts`、结果 `items` 和结构化 `labels`；组件统一顶部搜索框、常驻多选筛选、双列快捷入口、结果分组、高亮、空态及方向键 / Home / End / Enter 操作。`mobile` 隐藏快捷键提示，抽屉与遮罩仍由 `ModalScope` 约束。异步搜索通过 `busy/statusContent` 展示加载或错误，通过 `total/pagination` 提供总数和分页；检索、权限和跳转留在应用层。保留组件挂载并控制 `open`，以完成抽屉关闭动画。

`UIProvider` 可传入 `reducedMotion`，嵌套 Provider 默认继承。系统或应用任一要求减少动效时，交互 Logo 使用静态回退、抽屉跳过动画、横向选择直接定位。应用自己的 CSS 动效仍由应用控制。

`ConnectionLogo` 接受 `state: connected | reconnecting | disconnected | failed | recovered`，原型和 WebUI 共用动画与样式。正常状态沿用可交互的昼夜轮换；重连使用 loading；断开与失败保留原色并降至 24% 不透明度，失败轻摇一次；恢复使用单次招呼。`onRetry` 使断开/失败时的 Logo 支持点击及键盘重试，`labels/retryLabel/tooltipSide` 可定制状态提示。静态图案提前加载并保留在 DOM 中，离线时无需重新下载。减少动效只保留 loading。组件只展示状态，连接事件、超时和恢复计时由应用负责。

嵌套弹层关闭时，如果触发按钮已移除，会先回到仍然打开的弹层；异步数据更新移除当前焦点按钮时，Dialog 将焦点恢复到标题，避免 contained 抽屉丢失键盘控制。

### 日期与时间选择

`DateTimePicker` 使用 `value: string | null` 与 `onValueChange`，ISO 字符串表示时刻，`null` 表示无到期时间。复用次要 `Button` 展示紧凑连续按钮组，使用细边框与选中底色，窄屏保持单行并横向滚动。默认提供 `1d`、`7d`、`1m`、`forever` 快捷项，使用 `presets` 可裁剪；「自定义」打开项目内的日历浮层，复用 `Popover`、`Button` 与 `Select`，移动端跟随容器使用抽屉。采用左侧日历、右侧时分滚动列的双栏布局，支持月份／年份切换、日历键盘导航、时分滚动选择与最小时间校验，滚动时自动吸附并高亮选中值，支持点击数字与方向键／Home／End／PageUp／PageDown。确定后才提交；取消不改原值。选定日期显示在按钮中，不另显示第二行摘要。展示、编辑和快捷项使用 `timeZone` 指定的时区；未指定时使用浏览器本地时区，提交 ISO 时刻。1天和7天按该时区的日历推进，1个月按自然月推进，月底取目标月最后一个有效日；快捷项保留当前秒和毫秒。若目标时分因夏令时切换而不存在，显示错误并保留原值，不触发 `onValueChange`。`min` 接收 ISO 时刻；自定义选择会禁用早于最小时间的日期，所选时分不足时禁止确认。支持 `disabled`、`description`、`error` 与 `size="sm" | "md" | "lg"`。UI Design → 输入与操作中包含可操作示例。

按钮默认使用常规字重 `400`。日期按钮组直接沿用 Button 的 sm / md / lg 高度与内边距，不单独定义一套尺寸。


### RangeSlider 双端范围选择器

`RangeSlider` 在同一条轨道中选择闭区间，使用共享语义颜色、圆角和焦点样式，支持浅色／深色模式。UI Design → 输入与操作中提供标准、小号、禁用和手柄重合示例。

```tsx
const [range, setRange] = useState<[number, number]>([20, 80]);
<RangeSlider label="范围" min={0} max={100} step={5}
  value={range} onValueChange={setRange} formatValue={value => `${value}%`} />
```

- 必填：`label`、受控的 `value: readonly [number, number]`、`onValueChange`。
- `min` / `max` 默认 0 / 100，`step` 默认 1；变更按步长对齐，手柄不交叉。相同上下界作为不可调整的单值范围展示。
- `size` 为 `md`（默认）或 `sm`。支持 `disabled`、`hideLabel`、`description`、`error` 和 `className`。
- `lowerLabel` / `upperLabel` 可覆盖内置中英文手柄名称；`formatValue` 同时用于区间显示与 `aria-valuetext`，适用于时间戳、事件序号、百分比等。
- 鼠标或触摸可拖动任一手柄，点击轨道调整最近的一端。两个手柄重合时仍可分别用 Tab 聚焦，再向两侧展开。
- 方向键调整一个步长，PageUp / PageDown 调整十个步长；Home / End 到达当前手柄允许的边界。焦点顺序固定，不会随数值变动交换。

## 代码高亮

`CodeBlock` 接受 `code: string`，`language` 支持 `json` 与 `text`（默认 `text`），`wrap` 默认 `true`，`maxHeight` 默认 `28rem`，`label` 提供滚动区域的可访问名称。使用项目圆角、背景与明暗主题语法色；键名、字符串、数字、布尔值、null 和标点分别标记。 内容与显示属性未变化时复用当前实例的渲染结果，避免父级更新反复生成高亮节点；属性或界面语言变化仍正常更新。

```tsx
<CodeBlock language="json" code={JSON.stringify(payload, null, 2)} label="工具参数" />
<CodeBlock language="text" code={log} wrap={false} maxHeight={280} />
```

组件保留源字符串与空白，不执行代码、不插入 HTML。无效 JSON、超过 100,000 字符或超过约 10,000 个标记的内容回退为完整纯文本，限制高亮开销但不截断数据。需要格式化时由调用方显式提供缩进后的字符串。UI Design 的「设计基础 → 代码高亮」可切换格式、换行和编辑示例。

`ChatComposer.commands` 可选地提供 `{ name, description, keywords?, disabled? }[]`。组件负责输入开头的斜杠菜单、键盘与输入法行为，选择只补全文本；实际指令执行由调用方 `onSubmit` 根据完整输入分发。路径及正文中的斜杠保持普通输入。未传入 commands 的调用保持原有行为。

指令菜单复用 Select 浮层及选项样式，最大宽度 360 px，默认在输入框上方，不足时调整到下方。浮层使用原生非模态 popover，遵循 `ModalScope` 和 `visualViewport` 边界，焦点保留在 textarea；方向键仅滚动候选列表，触屏不显示键盘提示。示例入口为 UI Design `?components=chat#command-selector`。

### Slider 单端滑块

`Slider` 选择一个数值，支持鼠标、触控、方向键、PageUp/PageDown 和 Home/End。`min`、`max`、`value` 和 `onValueChange` 必填；`step` 默认为 1，最后一个不足步长的区间仍可选择精确上限。上下限相同时自动禁用，亦可显式传入 `disabled`。`labelExtra` 可放置帮助图标，`formatValue` 同时格式化数值、端点和无障碍朗读。颜色沿用共享语义 token。

```tsx
<Slider label="上下文压缩阈值" min={65536} max={209715} step={8192}
  value={value} onValueChange={setValue} formatValue={n => `${(n / 1024).toFixed(1)}K`} />
```

## 头像

`Avatar` 组合已有 `AssistantAvatar` 画作与圆形容器，接受 `name`、`avatar`（`preset:<名称>` 或 PNG/JPEG data URL）、`size`（默认 44 px）、`selected`、`notice` 与 `className`。字母头像选中时填色；预设和图片头像选中时使用 2 px 外圈描边、2 px 间隔，底层透明，避免抗锯齿边缘透出选中背景。交互由外层按钮提供，调用方为按钮设置名称与选中状态。

`notice` 可传入 `AvatarNotice`，其 `label` 提供悬停说明和读屏名称，`tone` 支持 `warning`（默认）与 `error`。圆点直径 10 px、背景描边 2 px，圆心位于头像右上方 45° 的圆周；`--avatar-notice-border` 默认使用 `--color-surface`，置于其它背景时由宿主指定对应语义背景色。WebUI 当前的有更新、版本不一致都使用 warning，error 仅预留。`--avatar-background` 可指定未选中字母头像的语义背景。

UI Design 的基础分类提供 `?components=foundations#avatars`，覆盖全部预设、字母、当前页面本地图片预览、尺寸、选中态与两种提醒色。WebUI 导航和该示例直接复用共享实现。

`ChatCopyButton` 支持可选 `label` 指定复制前的无障碍文案；`copyTextToClipboard(text)` 复用同一复制逻辑，包含局域网 HTTP 的同步回退。调用应由用户点击触发。
