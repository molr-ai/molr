import type { Locale } from './site-content';

type RouteRoot = 'docs' | 'dev-docs';
type SourceRoute = { file: string; path: string; label: string; group: string; englishLabel: string; englishGroup: string; root: RouteRoot };

const userSources: SourceRoute[] = [
  { file: 'README.md', path: '/docs/', label: '文档概览', group: '开始使用', englishLabel: 'Overview', englishGroup: 'Getting started', root: 'docs' },
  { file: 'getting-started.md', path: '/docs/getting-started/', label: '快速开始', group: '开始使用', englishLabel: 'Quick start', englishGroup: 'Getting started', root: 'docs' },
  { file: 'concepts.md', path: '/docs/concepts/', label: '基本概念', group: '开始使用', englishLabel: 'Core concepts', englishGroup: 'Getting started', root: 'docs' },
  { file: 'guides/assistants-and-chat.md', path: '/docs/guides/assistants-and-chat/', label: '助理与聊天', group: '日常使用', englishLabel: 'Assistants and chat', englishGroup: 'Everyday use', root: 'docs' },
  { file: 'guides/plans.md', path: '/docs/guides/plans/', label: '计划与执行', group: '日常使用', englishLabel: 'Plans and execution', englishGroup: 'Everyday use', root: 'docs' },
  { file: 'guides/memory.md', path: '/docs/guides/memory/', label: '记忆与自动整理', group: '日常使用', englishLabel: 'Memory and automatic review', englishGroup: 'Everyday use', root: 'docs' },
  { file: 'guides/permissions-and-budget.md', path: '/docs/guides/permissions-and-budget/', label: '权限与预算', group: '日常使用', englishLabel: 'Permissions and budgets', englishGroup: 'Everyday use', root: 'docs' },
  { file: 'guides/files-and-commands.md', path: '/docs/guides/files-and-commands/', label: '文件与命令', group: '日常使用', englishLabel: 'Files and commands', englishGroup: 'Everyday use', root: 'docs' },
  { file: 'guides/weixin.md', path: '/docs/guides/weixin/', label: '微信聊天入口', group: '日常使用', englishLabel: 'WeChat chat channel', englishGroup: 'Everyday use', root: 'docs' },
  { file: 'guides/plugins-and-skills.md', path: '/docs/guides/plugins-and-skills/', label: '插件与 Skills', group: '日常使用', englishLabel: 'Plugins and Skills', englishGroup: 'Everyday use', root: 'docs' },
  { file: 'guides/settings.md', path: '/docs/guides/settings/', label: '个人设置', group: '日常使用', englishLabel: 'Personal settings', englishGroup: 'Everyday use', root: 'docs' },
  { file: 'guides/account-security.md', path: '/docs/guides/account-security/', label: '账号安全', group: '日常使用', englishLabel: 'Account security', englishGroup: 'Everyday use', root: 'docs' },
  { file: 'administration/users-and-groups.md', path: '/docs/administration/users-and-groups/', label: '用户与用户组', group: '管理实例', englishLabel: 'Users and groups', englishGroup: 'Administration', root: 'docs' },
  { file: 'administration/models.md', path: '/docs/administration/models/', label: '连接与管理模型', group: '管理实例', englishLabel: 'Model connections', englishGroup: 'Administration', root: 'docs' },
  { file: 'administration/sandbox-resources.md', path: '/docs/administration/sandbox-resources/', label: '管理沙盒资源', group: '管理实例', englishLabel: 'Sandbox resources', englishGroup: 'Administration', root: 'docs' },
  { file: 'administration/plugins.md', path: '/docs/administration/plugins/', label: '插件管理', group: '管理实例', englishLabel: 'Plugin administration', englishGroup: 'Administration', root: 'docs' },
  { file: 'deployment/README.md', path: '/docs/deployment/', label: '部署与维护', group: '部署与维护', englishLabel: 'Deployment and maintenance', englishGroup: 'Deployment', root: 'docs' },
  { file: 'deployment/installation.md', path: '/docs/deployment/installation/', label: '安装与服务管理', group: '部署与维护', englishLabel: 'Installation and services', englishGroup: 'Deployment', root: 'docs' },
  { file: 'deployment/source-build.md', path: '/docs/deployment/source-build/', label: '源码构建与部署', group: '部署与维护', englishLabel: 'Building from source', englishGroup: 'Deployment', root: 'docs' },
  { file: 'deployment/configuration.md', path: '/docs/deployment/configuration/', label: '配置与访问', group: '部署与维护', englishLabel: 'Configuration and access', englishGroup: 'Deployment', root: 'docs' },
  { file: 'deployment/sandbox.md', path: '/docs/deployment/sandbox/', label: 'Linux 沙盒部署', group: '部署与维护', englishLabel: 'Linux sandbox deployment', englishGroup: 'Deployment', root: 'docs' },
  { file: 'deployment/remote-sandbox.md', path: '/docs/deployment/remote-sandbox/', label: '独立 Linux Sandbox 服务', group: '部署与维护', englishLabel: 'Standalone Linux Sandbox service', englishGroup: 'Deployment', root: 'docs' },
  { file: 'deployment/maintenance.md', path: '/docs/deployment/maintenance/', label: '更新与备份恢复', group: '部署与维护', englishLabel: 'Updates, backups and recovery', englishGroup: 'Deployment', root: 'docs' },
  { file: 'troubleshooting.md', path: '/docs/troubleshooting/', label: '故障排查', group: '故障排查', englishLabel: 'Troubleshooting', englishGroup: 'Troubleshooting', root: 'docs' },
];

const developerSources: SourceRoute[] = [
  { file: 'README.md', path: '/dev-docs/', label: '开发文档概览', group: '开始贡献', englishLabel: 'Developer overview', englishGroup: 'Contributing', root: 'dev-docs' },
  { file: 'contributing/documentation.md', path: '/dev-docs/contributing/documentation/', label: '文档维护约定', group: '开始贡献', englishLabel: 'Documentation maintenance', englishGroup: 'Contributing', root: 'dev-docs' },
  { file: 'architecture/overview.md', path: '/dev-docs/architecture/overview/', label: '架构总览', group: '架构', englishLabel: 'Architecture overview', englishGroup: 'Architecture', root: 'dev-docs' },
  { file: 'architecture/tech-stack.md', path: '/dev-docs/architecture/tech-stack/', label: 'Runtime 技术底座', group: '架构', englishLabel: 'Runtime technology stack', englishGroup: 'Architecture', root: 'dev-docs' },
  { file: 'runtime/agent-context.md', path: '/dev-docs/runtime/agent-context/', label: '自动上下文更新', group: '执行与数据', englishLabel: 'Automatic context updates', englishGroup: 'Execution and data', root: 'dev-docs' },
  { file: 'runtime/context-storage.md', path: '/dev-docs/runtime/context-storage/', label: '上下文存储', group: '执行与数据', englishLabel: 'Context storage', englishGroup: 'Execution and data', root: 'dev-docs' },
  { file: 'runtime/dream-memory.md', path: '/dev-docs/runtime/dream-memory/', label: '做梦与文件记忆', group: '执行与数据', englishLabel: 'Dreaming and file memory', englishGroup: 'Execution and data', root: 'dev-docs' },
  { file: 'runtime/model-adapters.md', path: '/dev-docs/runtime/model-adapters/', label: '模型后端适配', group: '执行与数据', englishLabel: 'Model adapters', englishGroup: 'Execution and data', root: 'dev-docs' },
  { file: 'runtime/multimodal-files.md', path: '/dev-docs/runtime/multimodal-files/', label: '按需读取多模态文件', group: '执行与数据', englishLabel: 'On-demand multimodal files', englishGroup: 'Execution and data', root: 'dev-docs' },
  { file: 'runtime/workbench.md', path: '/dev-docs/runtime/workbench/', label: '开发工作台', group: '执行与数据', englishLabel: 'Developer workbench', englishGroup: 'Execution and data', root: 'dev-docs' },
  { file: 'security/security-model.md', path: '/dev-docs/security/security-model/', label: 'Runtime 公网部署与安全边界', group: '安全与隔离', englishLabel: 'Runtime security model', englishGroup: 'Security and isolation', root: 'dev-docs' },
  { file: 'security/users-and-groups.md', path: '/dev-docs/security/users-and-groups/', label: '多用户、共享资源目录与成员配额', group: '安全与隔离', englishLabel: 'Users and shared resources', englishGroup: 'Security and isolation', root: 'dev-docs' },
  { file: 'security/resource-quotas.md', path: '/dev-docs/security/resource-quotas/', label: '资源授权额度', group: '安全与隔离', englishLabel: 'Resource quotas', englishGroup: 'Security and isolation', root: 'dev-docs' },
  { file: 'security/sandbox.md', path: '/dev-docs/security/sandbox/', label: 'Agent Shell 沙盒实现与边界', group: '安全与隔离', englishLabel: 'Agent Shell sandbox', englishGroup: 'Security and isolation', root: 'dev-docs' },
  { file: 'reference/http-api.md', path: '/dev-docs/reference/http-api/', label: 'Runtime HTTP 接口', group: '接口参考', englishLabel: 'Runtime HTTP API', englishGroup: 'API reference', root: 'dev-docs' },
  { file: 'reference/module-interfaces.md', path: '/dev-docs/reference/module-interfaces/', label: 'Runtime 模块 Public 接口', group: '接口参考', englishLabel: 'Runtime module interfaces', englishGroup: 'API reference', root: 'dev-docs' },
  { file: 'reference/file-tools.md', path: '/dev-docs/reference/file-tools/', label: '资源与文件工具合同', group: '接口参考', englishLabel: 'Resource and file tool contracts', englishGroup: 'API reference', root: 'dev-docs' },
  { file: 'extensions/plugins.md', path: '/dev-docs/extensions/plugins/', label: '插件机制（Plugin API 3）', group: '扩展', englishLabel: 'Plugin system (Plugin API 3)', englishGroup: 'Extensions', root: 'dev-docs' },
  { file: 'extensions/weixin.md', path: '/dev-docs/extensions/weixin/', label: '微信渠道', group: '扩展', englishLabel: 'WeChat channel', englishGroup: 'Extensions', root: 'dev-docs' },
  { file: 'frontend/architecture.md', path: '/dev-docs/frontend/architecture/', label: '前端架构', group: '前端', englishLabel: 'Frontend architecture', englishGroup: 'Frontend', root: 'dev-docs' },
  { file: 'frontend/themes.md', path: '/dev-docs/frontend/themes/', label: '主题与页面解析', group: '前端', englishLabel: 'Themes and page resolution', englishGroup: 'Frontend', root: 'dev-docs' },
  { file: 'frontend/brand.md', path: '/dev-docs/frontend/brand/', label: 'Molr 品牌与 UI 基础', group: '前端', englishLabel: 'Brand and UI foundations', englishGroup: 'Frontend', root: 'dev-docs' },
  { file: 'frontend/design-system.md', path: '/dev-docs/frontend/design-system/', label: '设计系统与颜色规范', group: '前端', englishLabel: 'Design system and colors', englishGroup: 'Frontend', root: 'dev-docs' },
  { file: 'frontend/interaction.md', path: '/dev-docs/frontend/interaction/', label: '前端交互约定', group: '前端', englishLabel: 'Frontend interactions', englishGroup: 'Frontend', root: 'dev-docs' },
  { file: 'releasing/releases.md', path: '/dev-docs/releasing/releases/', label: '发行维护', group: '发行维护', englishLabel: 'Release maintenance', englishGroup: 'Releases', root: 'dev-docs' },
  { file: 'releasing/public-content.md', path: '/dev-docs/releasing/public-content/', label: '开源发布与私有文件隔离', group: '发行维护', englishLabel: 'Public and private files', englishGroup: 'Releases', root: 'dev-docs' },
  { file: 'releasing/licensing.md', path: '/dev-docs/releasing/licensing/', label: 'MIT 许可证与第三方分发', group: '发行维护', englishLabel: 'MIT license and distribution', englishGroup: 'Releases', root: 'dev-docs' },
];

export type DocRoute = Omit<SourceRoute, 'englishLabel' | 'englishGroup'> & { locale: Locale; sourceFile: string };

function localize(sources: SourceRoute[], locale: Locale): DocRoute[] {
  return sources.map(({ englishLabel, englishGroup, ...source }) => ({
    ...source,
    locale,
    sourceFile: source.file,
    file: locale === 'en' ? `en/${source.file}` : source.file,
    path: `/${locale}${source.path}`,
    label: locale === 'en' ? englishLabel : source.label,
    group: locale === 'en' ? englishGroup : source.group,
  }));
}

export const docRoutes = localize(userSources, 'zh');
export const devDocRoutes = localize(developerSources, 'zh');
export const allDocRoutes = [...docRoutes, ...devDocRoutes, ...localize(userSources, 'en'), ...localize(developerSources, 'en')];
