import { memo } from 'react';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import remarkMath from 'remark-math';
import remarkBreaks from 'remark-breaks';
import rehypeKatex from 'rehype-katex';
import 'katex/dist/katex.min.css';
import './chat-markdown.css';

const remarkPlugins = [remarkGfm, remarkMath, remarkBreaks];
const rehypePlugins = [[rehypeKatex, { trust: false, strict: 'ignore', maxSize: 20, maxExpand: 1000 }]] as NonNullable<Parameters<typeof Markdown>[0]['rehypePlugins']>;

export const ChatMarkdown = memo(function ChatMarkdown({ text }: { text: string }) {
  return <div className="ui-chat-markdown"><Markdown remarkPlugins={remarkPlugins} rehypePlugins={rehypePlugins} components={{
    a: ({ node: _node, ...props }) => <a {...props} target="_blank" rel="noopener noreferrer" />,
    table: ({ node: _node, ...props }) => <div className="ui-chat-markdown__table" tabIndex={0}><table {...props} /></div>,
  }}>{text}</Markdown></div>;
});
