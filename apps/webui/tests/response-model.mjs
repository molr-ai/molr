// Deterministic provider responses exercised through the real Runtime.
export function responseModel(input, response) {
  if (!input.model?.startsWith('Response tabs ')) return false;
  const mode = input.model.split(' ')[2];
  const content = '模型正文第一行。\n第二行保持原始换行。';
  const thinking = '先检查输入。\n再给出结果。';
  const message = { role: 'assistant', content: mode === 'empty' || mode === 'tools' ? '' : content };
  let finish = 'stop';
  if (mode === 'tools' && !input.messages.some(m => m.role === 'tool')) {
    const event = JSON.parse(input.messages.findLast(m => m.content?.startsWith?.('Runtime event: ')).content.split('\n')[0].slice(15));
    message.tool_calls = [{ id: 'call-preview', type: 'function', function: { name: 'conversation_publish', arguments: JSON.stringify({ conversation_id: event.conversation_id, text: '工具调用完成。' }) } }];
    finish = 'tool_calls';
  }
  if (mode === 'stream') message.reasoning_content = thinking;
  const usage = { prompt_tokens: 30, completion_tokens: 10 };
  if (mode === 'json' || mode === 'empty' || mode === 'tools') {
    response.setHeader('Content-Type', 'application/json');
    response.end(JSON.stringify({ id: 'response-preview', choices: [{ message, finish_reason: finish }], usage }));
  } else {
    response.setHeader('Content-Type', 'text/event-stream');
    const send = value => response.write('data: ' + JSON.stringify({ id: 'response-preview', ...value }) + '\n\n');
    send({ choices: [{ index: 0, delta: { reasoning_content: thinking.slice(0, 6) } }] });
    send({ choices: [{ index: 0, delta: { reasoning_content: thinking.slice(6), content: content.slice(0, 5) } }] });
    send({ choices: [{ index: 0, delta: { content: content.slice(5) }, finish_reason: finish }] });
    if (mode !== 'interrupted') { send({ choices: [], usage }); response.write('data: [DONE]\n\n'); }
    response.end();
  }
  return true;
}
