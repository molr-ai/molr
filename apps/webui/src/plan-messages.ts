const zh = {
  returnToWeek: '返回本周', returnToMonth: '返回本月',
  week: '周', monthView: '月', weekView: '周日历', calendarMode: '日历模式', previousWeek: '上一周', nextWeek: '下一周', currentTime: '当前时间',
  noUpcomingHelp: '新增计划，安排接下来的事。',
  eventRange:'事件范围', eventList:'事件清单', noUndated:'没有未设日期的事项', viewUpcoming:'查看接下来的安排', noUpcoming:'没有即将发生的事件', noOverdue:'没有逾期未完成的安排', loadMoreEvents:'再加载 50 个事件', month:'查看月份', moreEvents:(count:number,day:string)=>`${day}：查看其余 ${count} 个事件`,
  actions: '计划操作', planDetails: '计划详情', closeDetails: '关闭详情', backToList: '返回计划列表', allOwners: '所有负责人', allTypes: '所有类型', allStatuses: '所有状态', clearFilters: '清除筛选', firstPlan: '添加第一项安排', emptyTitle: '安排接下来的工作', emptyHelp: '记下自己的事项，或交给助理。每次执行都会留在计划里。', filteredEmpty: '试试调整负责人或状态。', dayEmpty: '这一天还没有安排', dayEmptyHelp: '选择其他日期，或新增一项计划。', selectPlan: '选择一项计划查看详情', selectedDay: '当天安排', overviewMonths: '未来三个月', previousQuarter: '前 3 个月', nextQuarter: '后 3 个月', customRepeat: '自定义重复', latestExecution: '最近执行', executionCount: '次执行', plansCount: '项计划',

  overview: '我与助理的安排', recent: '近期', overdue: '逾期', upcoming: '接下来', later: '更远的安排', showLater: '展开更远安排', hideLater: '收起更远安排',
  executions: '执行记录', noExecutions: '尚未执行', startExecution: '开始本次执行', completeExecution: '完成本次执行', result: '结果或资料说明', executionDetails: '本次执行详情',
  dependencies: '前置计划', successors: '后续计划', handoff: '交接设置', notify: '提示我交接', autoHandoff: '自动启动后续执行', handoffHelp: '前置计划完成后显示待交接提示。自动启动需明确开启，并沿用助理已有权限和预算；不会自动授予资料访问权限。前置计划仅支持一次性安排。',
  blocked: '等待前置事项', ready: '待交接', pausePlan: '暂停后续安排', resumePlan: '恢复安排', planned: '已安排', unscheduled: '待安排', ended: '已结束',
  source: '所属计划', snapshot: '执行时的说明', selectOccurrence: '选择本次安排', preserveHistory: '已有执行记录或后续依赖的计划请暂停，历史记录将保留。',

  assistant: '助理', seriesEdit: '修改将应用到整个重复系列。', title: '计划', new: '新增计划', editor: '计划编辑', item: '事项', event: '时间安排', automatic: '自动执行',
  list: '清单', calendar: '日历', view: '计划视图', type: '计划类型', all: '全部', owner: '负责人', me: '我',
  status: '状态', open: '未完成', done: '已完成', name: '要做什么', details: '更多选项', due: '截止日期（可选）',
  block: '安排时间段', start: '开始时间', end: '结束时间', save: '保存计划', cancel: '取消', edit: '编辑', remove: '删除',
  notes: '备注', instructions: '执行说明', repeat: '重复', once: '不重复', daily: '每日', weekdays: '工作日', weekly: '每周',
  date: '查看日期', today: '今天', previous: '上个月', next: '下个月', pause: '暂停自动执行', enable: '启用自动执行', paused: '已暂停', enabled: '已启用',
  deleteHelp: '删除这项计划？重复安排将删除整个系列。', confirm: '确认删除', empty: '没有匹配的计划', undated: '未设日期',
  hint: '记录要做的事，按需设置截止日期或安排时间。', assignment: '负责人表示由谁跟进；需要定时让助理处理时，请设置自动执行。',
  automaticHelp: '按设定时间让助理执行，使用助理已有的权限和预算。', prototype: '原型仅在本浏览器保存计划，不会自动执行或发送提醒。',
  invalidTime: '请填写完整时间段，结束时间必须晚于开始时间。', saved: '计划已保存。', timezone: '时区', deadline: '截止', scheduled: '安排',
};
const en: typeof zh = {
  returnToWeek: 'Back to this week', returnToMonth: 'Back to this month',
  week: 'Week', monthView: 'Month', weekView: 'Week calendar', calendarMode: 'Calendar mode', previousWeek: 'Previous week', nextWeek: 'Next week', currentTime: 'Current time',
  noUpcomingHelp: 'Create a plan for what’s next.',
  eventRange:'Event range', eventList:'Event list', noUndated:'No undated items', viewUpcoming:'View upcoming plans', noUpcoming:'No upcoming events', noOverdue:'No overdue arrangements', loadMoreEvents:'Load 50 more events', month:'View month', moreEvents:(count:number,day:string)=>`${day}: show ${count} more events`,
  actions: 'Plan actions', planDetails: 'Plan details', closeDetails: 'Close details', backToList: 'Back to plan list', allOwners: 'All owners', allTypes: 'All types', allStatuses: 'All statuses', clearFilters: 'Clear filters', firstPlan: 'Add your first plan', emptyTitle: 'Make room for what’s next', emptyHelp: 'Plan something for yourself or an assistant. Every execution stays with its plan.', filteredEmpty: 'Try a different owner or status.', dayEmpty: 'Nothing arranged for this day', dayEmptyHelp: 'Choose another date or add a plan.', selectPlan: 'Select a plan to see its details', selectedDay: 'Day agenda', overviewMonths: 'Next three months', previousQuarter: 'Previous 3 months', nextQuarter: 'Next 3 months', customRepeat: 'Custom repeat', latestExecution: 'Latest execution', executionCount: 'executions', plansCount: 'plans',

  overview: 'Arrangements for me and my assistants', recent: 'Recent', overdue: 'Overdue', upcoming: 'Upcoming', later: 'Later arrangements', showLater: 'Show later arrangements', hideLater: 'Hide later arrangements',
  executions: 'Execution history', noExecutions: 'Not started', startExecution: 'Start this execution', completeExecution: 'Complete this execution', result: 'Result or handoff notes', executionDetails: 'Execution details',
  dependencies: 'Prerequisite plans', successors: 'Following plans', handoff: 'Handoff settings', notify: 'Prompt me to hand off', autoHandoff: 'Start automatically', handoffHelp: 'Completed prerequisites appear as ready to hand off. Automatic handoff must be explicitly enabled and uses existing assistant permissions and budget; it grants no access to materials. Prerequisites must be one-time plans.',
  blocked: 'Waiting for prerequisites', ready: 'Ready to hand off', pausePlan: 'Pause future arrangements', resumePlan: 'Resume arrangements', planned: 'Scheduled', unscheduled: 'Unscheduled', ended: 'Ended',
  source: 'Plan', snapshot: 'Instructions at execution', selectOccurrence: 'Choose occurrence', preserveHistory: 'Pause plans with execution history or dependents to retain their records.',

  assistant: 'Assistant', seriesEdit: 'Changes apply to the entire recurring series.', title: 'Plans', new: 'New plan', editor: 'Plan editor', item: 'Item', event: 'Time block', automatic: 'Automatic execution',
  list: 'List', calendar: 'Calendar', view: 'Plan view', type: 'Plan type', all: 'All', owner: 'Owner', me: 'Me',
  status: 'Status', open: 'Open', done: 'Completed', name: 'What needs doing?', details: 'More options', due: 'Due date (optional)',
  block: 'Schedule time', start: 'Start time', end: 'End time', save: 'Save plan', cancel: 'Cancel', edit: 'Edit', remove: 'Delete',
  notes: 'Notes', instructions: 'Instructions', repeat: 'Repeat', once: 'Does not repeat', daily: 'Every day', weekdays: 'Weekdays', weekly: 'Every week',
  date: 'View date', today: 'Today', previous: 'Previous month', next: 'Next month', pause: 'Pause automatic execution', enable: 'Enable automatic execution', paused: 'Paused', enabled: 'Enabled',
  deleteHelp: 'Delete this plan? Recurring arrangements will have their entire series deleted.', confirm: 'Confirm delete', empty: 'No matching plans', undated: 'No date',
  hint: 'Capture what needs doing, then add a deadline or schedule time when needed.', assignment: 'The owner follows up on an item. Set up automatic execution to have an assistant act at a scheduled time.',
  automaticHelp: 'Run at the scheduled time using the assistant’s existing permissions and budget.', prototype: 'This prototype saves plans in this browser only. It does not execute plans or send reminders.',
  invalidTime: 'Enter both times. The end must be after the start.', saved: 'Plan saved.', timezone: 'Timezone', deadline: 'Due', scheduled: 'Scheduled',
};
export const planMessages = { zh, en };
