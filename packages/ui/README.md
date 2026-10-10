# Molr UI

包名为 `@molr/ui`，提供共享视觉实现：

完整的组件目录、接口和示例见 [组件库文档](COMPONENTS.md)。UI Design 的 `#components` 提供输入、导航、反馈、弹层和数据展示五类可操作示例，包含通用组件以及资源卡片、任务卡片；设计基础（Foundations）标签页展示字体排印、圆角、间距与语义状态。

- `src/components/`：基础、输入、导航、反馈、弹层、数据展示、组合卡片与品牌组件。
- `src/locale.tsx`：`UIProvider` 与内置中英文标签。
- `src/use-theme.ts`：共享的系统主题跟随、偏好保存与同源标签页同步。
- `src/tokens.css`：浅色与暗色语义配色、字体、间距和动效变量。
- `src/styles.css`：变量与组件样式的统一入口。
- `public/brand/`：62 项 SVG 资源，包含静态、12 种独立动画、两套组合表演与圆角／直角应用图标。
- `public/brand/molr-icon-{size}.png` 与 `molr-icon-square-{size}.png`：圆角、直角两版应用图标，均提供 16、32、48、64、128、180、192、256、512、1024 px。圆角版保留透明圆角，直角版四角填满青柠底色；对应 SVG 为 `molr-icon.svg` 与 `molr-icon-square.svg`。
- `public/theme-init.js`：应用加载前的主题初始化脚本。
- `scripts/generate-logos.mjs`：Logo 的几何与配色来源。
- `src/logo-choreography.json`：新增动作的说明、两套预设的播放顺序、单轮时长、轮数与停顿。
- `scripts/logo-choreography.mjs`：将动作姿态编排成连续 SVG 时间轴。

在工作区应用中声明 `@molr/ui` 依赖后使用：

```tsx
import { Button, Logo } from '@molr/ui';
import '@molr/ui/styles.css';
```

共享 CSS 使用命名媒体查询，需要在消费应用的 PostCSS 配置中注册断点插件（两个 Vite 应用已配置）：

```ts
import breakpointQueries from '@molr/ui/postcss-breakpoints';
// vite.config.ts
export default defineConfig({
  css: { postcss: { plugins: [breakpointQueries()] } },
});
```

`@molr/ui/breakpoints.json` 是唯一宽度来源：`mobile` 为 700 px、`compact` 为 1000 px，均含边界。CSS 写 `@media (--molr-mobile)` 或 `@media (--molr-compact)`；JS 使用导出的 `breakpoints` 和 `useBreakpoint(name)`。其余现有布局断点也统一命名：`tiny` 380 px、`small` 500 px、`navigation` 800 px、`wide` 1180 px。媒体查询条件不支持 CSS 自定义属性，插件在开发和构建时将命名查询展开为标准 CSS。Hook 同一断点共享监听，最后一个订阅卸载时清理，SSR 返回 `false`。

在根元素设置 `data-theme="dark"` 使用暗色主题，`data-theme="light"` 使用浅色主题（默认）。品牌原始色保持固定，背景、表面、文字、边框、焦点和状态色通过语义变量切换。共享 [`useTheme`](src/use-theme.ts) 提供主题状态，`ThemeSelect` 可提供切换入口；以下是 UI Design 的组合用法：

```tsx
const { preference, theme, changePreference } = useTheme();
<ThemeSelect value={preference} onValueChange={changePreference} />
<Logo variant={theme === 'dark' ? 'light' : 'dark'} animation="daily" interactive />
```

`ThemeSelect` 默认继承 `UIProvider` 的语言，未提供 Provider 时为 `zh`；也可显式传入 `locale="en"` 将标签、选项与说明切换为英文。主题值与保存的偏好不受语言切换影响。

在 HTML 的 `theme-color` 元信息之后加载 `/theme-init.js`，可提前应用主题，避免首次绘制闪白；该脚本随本包 `public/` 一起提供。`useTheme` 在应用挂载后接管主题状态；同一页面的多个调用共享外观偏好，手动切换立即同步所有 Logo 和主题控件，也保留跨标签页同步与存储不可用时的内存状态。暗色背景上的 Logo 使用 `variant="light"`；青柠底和浅色底使用默认深绿 Logo。

## 输入补全

`TextField`、`Textarea` 和 `SearchField` 默认使用 `autoComplete="off"`，业务名称、搜索词、模型配置等字段不复用浏览器的个人资料或历史输入。登录用户名、登录密码、新密码和验证码应显式指定 `username`、`current-password`、`new-password` 和 `one-time-code`，并提供对应的 `name`。

API Key 和服务 Token 使用 `SecretField`，传入按提供商或连接区分的 `name`；切换凭证上下文时同时更换 React `key`。组件默认遮盖内容，提供“显示／隐藏”按钮查看当前输入原文；更换凭证上下文或重新挂载时恢复隐藏。组件关闭补全、自动纠正和拼写检查，提示密码管理器忽略该字段，并在支持 `-webkit-text-security` 的浏览器里遮盖普通文本输入，避免作为登录密码字段识别。不支持时回退到原生密码遮盖及独立的 `new-password` 分组。浏览器扩展是否尊重忽略提示仍由扩展决定。密钥草稿只保存在调用方内存中，不应写入浏览器持久存储。

## 下拉选择

`Select` 提供统一的单选／多选触发器和弹出列表，用于主题、视图、排序等设置。选项支持图标、辅助说明与禁用状态；浅色、暗色共用语义配色。值由调用方控制：

所有业务下拉框（包括筛选、表单、聊天模型与主题选择）复用此组件。触发器使用表面色底、8 px 圆角与弱边框，悬停轻微加深；聚焦或展开时沿原边框向内加粗，不叠加外部光圈。错误状态在聚焦、展开时仍保留错误色。主题选择不再单独使用透明底。

下拉列表使用无描边表面与轻阴影，选项采用 6 px 圆角、9 px／10 px 内边距；当前值使用底色、字重和勾选标记，悬停、键盘活动项和选中项共用同一背景色，选项间距为 4 px。单选时，活动项与已选值相互独立，取消展开不会提交新值。聊天模型选择仅缩小触发器尺寸，复用同一套菜单与键盘交互。

```tsx
import { Select } from '@molr/ui';

<Select
  label="展示方式"
  name="view"
  value={view}
  onValueChange={setView}
  options={[
    { value: 'list', label: '列表', description: '逐项浏览内容' },
    { value: 'board', label: '看板' },
    { value: 'timeline', label: '时间线', disabled: true },
  ]}
/>
```

| 属性 | 约定 |
| --- | --- |
| `label` | 必填的可读标签；`hideLabel` 只隐藏视觉标签 |
| `value` / `onValueChange` | 受控选中值与确认回调；空字符串可显示占位文字 |
| `options` | 只读选项数组，每项有 `value`、`label`，可带 `icon`、`description`、`disabled` |
| `placeholder` | 未选择时的文字，默认「请选择」 |
| `description` / `error` | 字段说明或错误；错误优先显示并设置 `aria-invalid` |
| `disabled` | 禁用整个控件；空列表或全部选项禁用时也不可打开 |
| `size` / `align` | `sm` 为 40 px，`md`（默认）为 44 px；列表按 `start`（默认）或 `end` 对齐 |
| `name` / `id` / `className` | 表单隐藏值、触发器 ID 和根元素样式类 |

同时导出泛型 `SelectProps<Value>`、`SelectOption<Value>`。调用方负责值和错误校验；传入 `name` 可让启用状态的选中值进入 `FormData`。导航中的主题选择使用同一组件，窄屏只显示所选图标，展开后显示完整选项。

交互采用 [WAI 单选 Combobox 模式](https://www.w3.org/WAI/ARIA/apg/patterns/combobox/examples/combobox-select-only/)：方向键移动，Home / End 到首末项，PageUp / PageDown 按十项移动，输入字符按标签或值的前缀查找；Enter / 空格确认，Tab 确认后继续移动焦点，Escape 或点击外部取消。禁用项会被跳过，浏览选项期间保持原值。列表通过 `aria-activedescendant` 表达当前活动项，键盘焦点保留在触发器。

弹层使用浏览器 Popover API 进入顶层，避免容器裁切；按可用空间向上或向下展开，限制在视口内，长列表可以滚动。组件不依赖额外 UI 库，需使用支持 Popover API 的现代浏览器。选项图标由调用方传入装饰性 React 节点。

## Logo

`Logo` 默认静态显示，`animation` 可与 `variant="light"`、`markOnly` 组合。动画内置于 SVG，系统启用减少动态效果时显示静态资源。

| `animation` | 动作 | 播放方式 |
| --- | --- | --- |
| `static` | 静态（默认） | 静止 |
| `blink` | 眨眼 | 4 秒循环 |
| `idle` | 休闲，猫耳和尾梢轻柔舒展 | 2.8 秒循环 |
| `curious` | 好奇，单耳轻抬、歪头回正 | 1.8 秒，播放一次 |
| `sleepy` | 打盹，双眼眯成弧线、耳尖放松，眯眼保持 20.4 秒 | 24 秒，播放一次 |
| `greet` | 招呼，点头眨眼、尾梢轻摆 | 2.4 秒，播放一次；独立 M 用点头眨眼回应 |
| `loading` | 加载，o 的环形描边配合猫耳舒展、尾梢慢摆；独立 M 使用自身轮廓描边 | 描边 1.6 秒一圈，形变 3.2 秒循环 |
| `look` | 张望，双眼与头部一起观察两侧 | 3.6 秒，播放一次 |
| `listen` | 侧耳听，单耳竖起并轻动 | 2 秒，播放一次 |
| `stretch` | 伸懒腰，先收身再舒展，尾梢稍后跟随 | 3.2 秒，播放一次 |
| `nuzzle` | 轻蹭，眯眼、侧靠、收拢尾梢 | 2.6 秒，播放一次 |
| `earflick` | 抖耳，耳尖先后轻动再眨眼 | 1.6 秒，播放一次 |
| `peek` | 探头，微微前探、睁大眼睛、抬起尾尖 | 2.8 秒，播放一次 |
| `alive` | 安静好奇的预设表演 | 299.6 秒循环，动作间停顿 20～30 秒 |
| `playful` | 亲近俏皮的预设表演 | 200.4 秒循环，动作间停顿 15～25 秒 |
| `daily` | 昼夜轮换：本地时间 08:00–20:00 安静好奇，20:00–次日 08:00 亲近俏皮 | 在 08:00、20:00 自动切换 |

```tsx
<Logo animation="idle" />
<Logo markOnly variant="light" animation="idle" />
<Logo animation="curious" replayKey={interactionCount} />
<Logo animation="daily" />
<Logo animation="daily" interactive />
<Logo markOnly animation="playful" />
```

`replayKey` 是可选的数字。调用方在悬停、聚焦或其他交互时递增自己的计数（上例的 `interactionCount`），组件会用新的图片 URL 重新播放内置动画。好奇、打盹、招呼和六个新增独立动作播放完后归位；下载的 SVG 保留同样的单次播放行为。

两套表演都编译为一个完整 SVG 时间轴，直接通过 `<img>` 使用即可连续播放。修改 `logo-choreography.json` 中对应预设的 `steps` 顺序、`duration`（单轮秒数）、`cycles`（连续播放轮数，默认 1）和 `pause`（整段完成后的停顿秒数），再运行 `npm run brand:generate` 即可重新编排。每轮动作从静止姿态开始并归位，循环边界保持连续；同一动作的各轮之间不插入额外停顿。

组合中的眨眼、休闲支持连续播放多轮：安静好奇预设为休闲 5 轮、眨眼 4 轮；亲近俏皮预设为眨眼 3 轮。打盹只播放一次，在 24 秒内保持眯眼 20.4 秒，闭眼和睁眼速度不变；其余动作保持单次回应。亲近俏皮将眨眼连播放在抖耳与轻蹭之间。段后等待为 15～30 秒。轮数和停顿固定写入预设，刷新不会随机变化；生成器会拒绝无效轮数或给单次动作设置多轮。

`daily` 由 React 组件按设备本地时间选择预设：08:00（含）至 20:00（不含）为 `alive`，其余时间为 `playful`。页面会在 08:00、20:00 更新，后台标签页恢复时也会核对时段；手动指定预设始终播放该套。夜间跨过午夜不会重新开始动画。此模式没有单独的 `-daily.svg`，下载的是当前时段选中的固定预设。展示页与组件共用 `useDailyLogoPreset`，也可调用 `getDailyLogoPreset(date)` 取得指定本地时刻的预设及 `slotKey`。

组合动画可用 `interactive` 启用点击交互，适用于 `alive`、`playful` 和 `daily`。在一段动作完成后的等待间隔中，点击、触摸或聚焦后按 Enter / 空格，会立即播放下一阶段；末段等待会跳到下一轮的开头。正在播放的动作及其 3～5 轮连播会正常完成，连续点击不会跳过它们。

交互 Logo 是独立按钮，应避免放入链接或其他按钮中。组件不显示状态文字和悬停提示，通过鼠标指针、键盘焦点及读屏标签表达可操作状态。

默认读屏标签跟随 `UIProvider.locale`，区分准备、播放和等待状态，动作名称同步翻译。多语言页面也可用 `interactionLabel` 覆盖动画按钮的读屏操作名称，例如 `interactionLabel="Molr, play the next animation"`。更新该文本不重启动画；静态及减少动态效果的回退仍以品牌名称标识图片。

交互组件通过同源 `<object>` 读取 SVG 的实际播放时间并定位到下一阶段，保持画面连续。资源不可控时回退为普通图片播放；系统启用减少动态效果时显示静态标识。默认 Logo 仍使用 `<picture>` / `<img>`，单独下载的 SVG 保留自动播放，点击跳过等待由组件提供。

实际加载场景搭配状态文字：

```tsx
<div role="status">
  <Logo markOnly animation="loading" />
  <span>正在加载…</span>
</div>
```

只需要变量时可导入 `@molr/ui/tokens.css`。包导出 TypeScript 源码，由消费应用的构建工具编译，无独立发布或构建步骤；React 由消费应用提供。

消费应用负责加载字体、设置页面基础样式，并选择品牌资产部署方式。Logo 默认从 `/brand` 加载；部署到子路径时，可统一配置 `<UIProvider brandAssetBase="/app/brand">`，或给单个 Logo 传 `assetBaseUrl`。静态图、动画图和减少动态效果的回退共用该目录，replay/period 参数照常生效。当前 Vite 应用使用 `import.meta.env.BASE_URL` 设置目录。

包通过 `@molr/ui/brand/*` 导出品牌文件，例如 Vite 中可用 `import markUrl from '@molr/ui/brand/molr-mark.svg?url'`，也可在构建时解析导出路径后复制到自己的静态目录。默认 `/brand` 是兼容现有展示页的约定，不再是唯一部署位置。交互 SVG 仍需同源才能读取动画时钟，跨域资源会使用既有图片回退。Vite 配置示例见 [UI Design](../../apps/ui-design/vite.config.ts)。组件内部不依赖展示页、Runtime、平台 API 或真实凭证。

交互 Logo 仅在页面可见时轮询 SVG 时钟；页面隐藏后停止轮询，恢复可见时立即读取真实播放位置。减少动态效果、图片回退及卸载时清理计时器和可见性监听。`apps/ui-design/tests/logo-visibility.spec.ts` 覆盖后台加载、恢复、点击跳过等待及清理。

在仓库根目录运行 `npm run brand:generate` 重新导出 Logo，运行 `npm run typecheck --workspace @molr/ui` 检查组件类型。视觉约定见 [品牌规范](../../dev-docs/frontend/BRAND.md)。

`brand:generate` 使用 `@resvg/resvg-js` 从同一份应用图标 SVG 生成全部 PNG；自定义配色同时应用到两种格式。尺寸列表统一维护在 `src/app-icon-sizes.json`，并通过 `appIconSizes` 导出供下载界面使用。

`Select` 传入 `multiple` 后，`value` 与 `onValueChange` 使用数组；点击、Enter 或空格可连续切换选中，列表保持打开，Tab／Escape 关闭。选中项使用普通列表行和勾选图标，不添加复选框。`trigger` 可传入自定义图标按钮，复用相同的定位、焦点和键盘逻辑。

运行 `npm run test --workspace @molr/ui` 检查无浏览器全局对象时的服务端渲染、资产导出及播放边界。播放查询对零/负/非有限 duration、空 steps、非有限 elapsed 和未覆盖时间点返回 null；交互 Logo 遇到无效播放状态会显示图片回退。

### 配色主题

`PalettePicker` 提供六套主题预览卡，UI Design 与 WebUI 复用同一组件和 `palettes.css`。`usePalettePreference(storageKey, theme, brandBase)` 在应用根部应用配色、同步浏览器偏好与 SVG Logo 地址；账号级持久化由应用完成。配色样式仅匹配 `data-palette`，未启用的官网保持原样。Vite 应用通过 `@molr/ui/themed-brand-plugin` 提供主题 SVG 资源并输出静态构建文件，PNG 原始资产不变。

## 构建副作用边界

包清单使用 `sideEffects: ["**/*.css"]`，保留所有显式 CSS 导入，同时允许构建器移除未使用的组件及其依赖。组件模块没有必须在导入时执行的全局初始化；浏览器事件监听和运行时状态初始化应留在组件生命周期或显式调用中。新增不可移除的初始化模块时，必须将其加入清单，不能依赖偶然的导入顺序。SSR 导入与组件行为测试、三个应用的生产构建共同检查这一边界。
