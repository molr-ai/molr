export type ChatComposerCommand = {
  name: string;
  description: string;
  label?: string;
  aliases?: readonly string[];
  keywords?: readonly string[];
  disabled?: boolean;
  children?: readonly ChatComposerCommand[];
};
export type ComposerCommandMatch = ChatComposerCommand & { completion: string; hasChildren: boolean };

/** Resolve only the current level. Parent tokens must match a command or alias exactly. */
export function resolveComposerCommands(value: string, commands: readonly ChatComposerCommand[]): { prefix: string; matches: ComposerCommandMatch[] } | undefined {
  const query = /^\/([^/\r\n]*)$/.exec(value)?.[1];
  if (query === undefined) return undefined;
  const tokens = query.split(/\s+/), search = tokens.pop()!.toLocaleLowerCase();
  let level = commands, preferAlias = false;
  const path: string[] = [];
  for (const token of tokens) {
    const parent = level.find(command => [command.name, ...(command.aliases ?? [])].some(name => name.toLocaleLowerCase() === token.toLocaleLowerCase()));
    if (!parent || parent.disabled || !parent.children?.length) return { prefix: path.join(' '), matches: [] };
    path.push(token);
    preferAlias = token.toLocaleLowerCase() !== parent.name.toLocaleLowerCase();
    level = parent.children;
  }
  const matches = level.flatMap(command => {
    const preferred = preferAlias ? command.aliases?.[0] ?? command.name : command.name;
    const name = search ? [command.name, ...(command.aliases ?? [])].find(name => name.toLocaleLowerCase().startsWith(search)) : preferred;
    if (!name && ![command.label ?? '', ...(command.keywords ?? [])].some(text => text.toLocaleLowerCase().includes(search))) return [];
    const selected = name ?? preferred, hasChildren = !!command.children?.length;
    return [{ ...command, name: selected, completion: `/${[...path, selected].join(' ')}${hasChildren ? ' ' : ''}`, hasChildren }];
  });
  return { prefix: path.join(' '), matches };
}
