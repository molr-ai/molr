import type { FullAccessScope, Grant, PermissionRequest, PermissionRequestInput, Resource, SandboxView } from '@molr/sdk';

const expires = new Date(Date.now() + 3 * 60 * 60 * 1000).toISOString();
const programs = (...rules: NonNullable<FullAccessScope['programs']>): FullAccessScope => ({ mode: 'programs', programs: rules });
const input = (scope: FullAccessScope | undefined, reason: string, resourceID = 'local-compute'): PermissionRequestInput => ({
  full_access: true, full_access_scope: scope, resource_kind: 'compute', resource_id: resourceID, resource_description: 'Local compute', actions: ['execute'], expires_at: expires, reason,
});
export type CardCase = { id: string; zh: string; en: string; request: PermissionRequest };
const makeCase = (id: string, zh: string, en: string, proposal: PermissionRequestInput, state: PermissionRequest['State'] = 'pending', granted?: FullAccessScope, permanent = false): CardCase => {
  const grant: Grant | undefined = state === 'approved' ? { ID: `grant-${id}`, AssistantID: 'preview-assistant', ResourceID: proposal.resource_id!, Actions: ['execute'], Scope: { FullAccess: true, FullAccessScope: granted ?? proposal.full_access_scope, ModelID: '', PathPrefixes: [], ReadOnly: false }, RevokedAt: null, ExpiresAt: permanent ? null : expires, Version: 1 } : undefined;
  return { id, zh, en, request: { ID: `permission-${id}-0123456789abcdef0123456789abcdef`, AssistantID: 'preview-assistant', TaskID: 'preview-task', State: state, GrantID: grant?.ID ?? '', MessageID: `message-${id}`, Granted: grant, GrantedAll: grant ? [grant] : [], CreatedAt: new Date().toISOString(), ResolvedAt: state === 'pending' ? null : new Date().toISOString(), Version: 1, Input: proposal } };
};

export function createCardCases(): CardCase[] {
  const cases = [
    makeCase('unrestricted', '不限范围', 'Unrestricted', input({ mode: 'unrestricted' }, '需要整理宿主机上的项目文件，并运行构建和发布命令。')),
    makeCase('any-arguments', '指定程序 · 任意参数', 'Program · Any arguments', input(programs({ executable: '/usr/bin/ffmpeg', arguments: 'any' }), '使用已安装的 FFmpeg 处理素材，允许按转换任务选择参数。')),
    makeCase('exact-arguments', '指定程序 · 固定参数', 'Program · Exact arguments', input(programs({ executable: '/usr/bin/ffmpeg', arguments: 'exact', args: ['-i', '/work/source recording.mp4', '-c:v', 'libx264', '/work/output.mp4'] }), '把这段视频转换为 H.264，仅执行下面这一组参数。')),
    makeCase('no-arguments', '指定程序 · 无参数', 'Program · No arguments', input(programs({ executable: '/usr/bin/uptime', arguments: 'exact', args: [] }), '查看主机运行时长，仅允许无参数启动 uptime。')),
    makeCase('multiple-programs', '多个程序 · 混合参数规则', 'Multiple programs · Mixed rules', input(programs({ executable: '/usr/bin/ffmpeg', arguments: 'any' }, { executable: '/usr/bin/ffprobe', arguments: 'exact', args: ['-version'] }), '转换视频并检查媒体工具版本，每个程序使用各自的参数范围。')),
    makeCase('special-arguments', '空参数、空格与换行', 'Empty arguments, spaces and newlines', input(programs({ executable: '/usr/bin/printf', arguments: 'exact', args: ['%s\n', '', '  keep spaces  ', 'first line\nsecond line'] }), '核对输出格式，需要保留每个参数的边界与空白字符。')),
    makeCase('windows-path', '远程节点 · Windows 长路径', 'Remote node · Long Windows path', input(programs({ executable: 'C:\\Program Files\\Media Tools\\Video Processing Suite\\bin\\ffmpeg.exe', arguments: 'exact', args: ['-i', 'D:\\Shared Media\\Source recordings\\interview with a long filename.mp4', 'D:\\Shared Media\\Exports\\interview.mp4'] }), '在 Windows 工作站转换共享目录中的录制文件。', 'windows-compute')),
    makeCase('missing-scope', '未声明范围 · 待补全', 'Missing scope · Needs review', input(undefined, '申请在宿主环境执行任务，授权范围尚待确认。')),
    makeCase('approved-narrowed', '已批准 · 范围已收窄', 'Approved · Narrowed scope', input({ mode: 'unrestricted' }, '检查主机上的媒体工具版本。'), 'approved', programs({ executable: '/usr/bin/ffmpeg', arguments: 'exact', args: ['-version'] })),
    makeCase('approved-permanent', '已批准 · 不限范围与有效期', 'Approved · Unrestricted, no expiry', input({ mode: 'unrestricted' }, '维护专用于构建任务的主机环境。'), 'approved', { mode: 'unrestricted' }, true),
    makeCase('rejected', '已拒绝', 'Rejected', input(programs({ executable: '/usr/bin/python3', arguments: 'any' }), '使用宿主机的 Python 执行数据处理脚本。'), 'rejected'),
    makeCase('grouped', '组合申请 · 程序与目录', 'Grouped · Program and directory', input(programs({ executable: '/usr/bin/ffmpeg', arguments: 'any' }), '转换素材，并通过文件工具整理输出目录。')),
  ];
  cases.at(-1)!.request.Input.additional_requests = [{ resource_kind: 'storage', resource_id: 'output-directory', resource_description: '视频输出目录', actions: ['read', 'write'], path_prefixes: ['/exports'], expires_at: expires, reason: '读取转换结果并整理交付文件。' }];
  const one = makeCase('once-pending', '仅执行一次 · 待审批', 'One use · Pending', input(programs({ executable: '/usr/bin/printf', arguments: 'exact', args: ['verified'] }), '仅执行一次固定的输出验证。'));
  one.request.Input.once = true;
  const unused = makeCase('once-unused', '仅执行一次 · 尚未使用', 'One use · Not used', input(programs({ executable: '/usr/bin/uptime', arguments: 'exact', args: [] }), '查看一次主机运行时长。'), 'approved');
  unused.request.Input.once = true; unused.request.Granted!.Scope.Once = true;
  const used = makeCase('once-used', '仅执行一次（已使用）', 'Use once (used)', input(programs({ executable: '/usr/bin/printf', arguments: 'exact', args: ['verified'] }), '这次验证已经使用了授权。'), 'approved');
  used.request.Input.once = true; used.request.Granted!.Scope.Once = true;
  used.request.Granted!.Used = { OperationID: 'preview-used-operation', ExecutionID: 'preview-execution', UsedAt: new Date().toISOString() };
  const onceHost = makeCase('once-unrestricted', '不限范围 · 仅一次', 'Unrestricted · One use', input({ mode: 'unrestricted' }, '仅启动一次宿主命令或终端会话。'));
  onceHost.request.Input.once = true;
  cases.push(one, unused, used, onceHost);
  return cases;
}
export const resources: Resource[] = [
  { ID: 'local-compute', Kind: 'compute', Name: 'Local compute', Provider: 'local-sandbox', State: 'available', Actions: ['execute'], Version: 1 },
  { ID: 'windows-compute', SandboxID: 'windows', Kind: 'compute', Name: 'Windows compute', Provider: 'remote-compute', State: 'available', Actions: ['execute'], Version: 1 },
  { ID: 'output-directory', Kind: 'storage', Name: '视频输出目录', Provider: 'local-directory', State: 'available', Actions: ['read', 'write'], Version: 1 },
];
export const sandboxes = [
  { ID: 'local', Name: 'Local sandbox', Local: true, State: 'available', Info: { Enabled: true, Capabilities: { FullAccess: true, Shell: true } } },
  { ID: 'windows', Name: 'Windows 工作站', Local: false, State: 'available', Info: { Enabled: true, Capabilities: { FullAccess: true, Shell: true, OS: 'windows' } } },
] as SandboxView[];
