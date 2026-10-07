type RouteRoot = 'docs' | 'dev-docs';
export type DocRoute = { file: string; path: string; label: string; group: string; root: RouteRoot };

export const docRoutes: DocRoute[] = [
  { file: 'README.md', path: '/docs/', label: '文档概览', group: '开始使用', root: 'docs' },
  { file: 'getting-started.md', path: '/docs/getting-started/', label: '快速开始', group: '开始使用', root: 'docs' },
  { file: 'architecture.md', path: '/docs/architecture/', label: '架构总览', group: '开始使用', root: 'docs' },
  { file: 'users-and-groups.md', path: '/docs/users-and-groups/', label: '用户与用户组', group: '使用指南', root: 'docs' },
  { file: 'assistants-and-plans.md', path: '/docs/assistants-and-plans/', label: '助理、聊天与计划', group: '使用指南', root: 'docs' },
  { file: 'resources-and-permissions.md', path: '/docs/resources-and-permissions/', label: '资源、权限与预算', group: '使用指南', root: 'docs' },
  { file: 'weixin.md', path: '/docs/weixin/', label: '微信聊天入口', group: '使用指南', root: 'docs' },
  { file: 'plugins.md', path: '/docs/plugins/', label: '插件', group: '使用指南', root: 'docs' },
  { file: 'settings-and-troubleshooting.md', path: '/docs/settings-and-troubleshooting/', label: '设置、安全与常见问题', group: '使用指南', root: 'docs' },
  { file: 'deployment/README.md', path: '/docs/deployment/', label: '部署与维护', group: '部署与维护', root: 'docs' },
  { file: 'deployment/multi-user-upgrade.md', path: '/docs/deployment/multi-user-upgrade/', label: '从单账号升级到多用户', group: '部署与维护', root: 'docs' },
  { file: 'deployment/sandbox.md', path: '/docs/deployment/sandbox/', label: 'Linux 命令环境', group: '部署与维护', root: 'docs' },
  { file: 'deployment/remote-sandbox.md', path: '/docs/deployment/remote-sandbox/', label: '独立 Sandbox 服务', group: '部署与维护', root: 'docs' },
];

export const devDocRoutes: DocRoute[] = [
  { file: 'README.md', path: '/dev-docs/', label: '开发文档概览', group: '概览', root: 'dev-docs' },
  { file: 'PRODUCT_SCOPE.md', path: '/dev-docs/product-scope/', label: '产品范围', group: '产品', root: 'dev-docs' },
  { file: 'runtime/ARCHITECTURE.md', path: '/dev-docs/runtime/architecture/', label: 'Runtime 架构', group: 'Runtime', root: 'dev-docs' },
  { file: 'runtime/TECH_STACK.md', path: '/dev-docs/runtime/tech-stack/', label: '技术底座', group: 'Runtime', root: 'dev-docs' },
  { file: 'runtime/INTERFACES.md', path: '/dev-docs/runtime/interfaces/', label: '模块接口', group: 'Runtime', root: 'dev-docs' },
  { file: 'runtime/HTTP_API.md', path: '/dev-docs/runtime/http-api/', label: 'HTTP API', group: 'Runtime', root: 'dev-docs' },
  { file: 'runtime/MULTI_USER.md', path: '/dev-docs/runtime/multi-user/', label: '多用户与成员配额', group: 'Runtime', root: 'dev-docs' },
  { file: 'runtime/WEIXIN.md', path: '/dev-docs/runtime/weixin/', label: '微信渠道', group: 'Runtime', root: 'dev-docs' },
  { file: 'runtime/PLUGINS.md', path: '/dev-docs/runtime/plugins/', label: '插件机制', group: 'Runtime', root: 'dev-docs' },
  { file: 'runtime/RESOURCE_QUOTAS.md', path: '/dev-docs/runtime/resource-quotas/', label: '资源授权额度', group: 'Runtime', root: 'dev-docs' },
  { file: 'runtime/SECURITY.md', path: '/dev-docs/runtime/security/', label: '安全边界', group: 'Runtime', root: 'dev-docs' },
  { file: 'runtime/SANDBOX.md', path: '/dev-docs/runtime/sandbox/', label: '沙盒设计', group: 'Runtime', root: 'dev-docs' },
  { file: 'runtime/FILE_TOOLS.md', path: '/dev-docs/runtime/file-tools/', label: '文件与资源工具', group: 'Runtime', root: 'dev-docs' },
  { file: 'runtime/AGENT_CONTEXT.md', path: '/dev-docs/runtime/agent-context/', label: '上下文更新', group: 'Runtime', root: 'dev-docs' },
  { file: 'runtime/MODEL_ADAPTERS.md', path: '/dev-docs/runtime/model-adapters/', label: '模型适配', group: 'Runtime', root: 'dev-docs' },
  { file: 'runtime/MULTIMODAL_FILES.md', path: '/dev-docs/runtime/multimodal-files/', label: '多模态文件', group: 'Runtime', root: 'dev-docs' },
  { file: 'runtime/WORKBENCH.md', path: '/dev-docs/runtime/workbench/', label: '开发工作台', group: 'Runtime', root: 'dev-docs' },
  { file: 'runtime/DREAM_MEMORY.md', path: '/dev-docs/runtime/dream-memory/', label: '夜间做梦与记忆', group: 'Runtime', root: 'dev-docs' },
  { file: 'frontend/BRAND.md', path: '/dev-docs/frontend/brand/', label: '品牌规范', group: '前端', root: 'dev-docs' },
  { file: 'frontend/UI_PACKAGES.md', path: '/dev-docs/frontend/ui-packages/', label: '主题与 UI 包', group: '前端', root: 'dev-docs' },
];
