import type { ReactNode } from 'react';
import { useUILocale } from '../locale';
import { Checkbox } from './forms';
import { Progress } from './feedback';
import { UIIcon } from './icons';

export type TableColumn<Row> = { key: string; header: string; cell: (row: Row) => ReactNode; sortable?: boolean; align?: 'start' | 'end' };
export type TableSort = { column: string; direction: 'asc' | 'desc' };
export type TableSelection<Row> = { keys: readonly string[]; onChange: (keys: string[]) => void; rowLabel: (row: Row) => string; isDisabled?: (row: Row) => boolean };
export type TableProps<Row> = {
  caption: string; columns: readonly TableColumn<Row>[]; rows: readonly Row[]; rowKey: (row: Row) => string;
  sort?: TableSort; onSortChange?: (sort: TableSort) => void; selection?: TableSelection<Row>;
  emptyState?: ReactNode; loading?: boolean; className?: string;
};

export function Table<Row>({ caption, columns, rows, rowKey, sort, onSortChange, selection, emptyState, loading = false, className = '' }: TableProps<Row>) {
  const { messages } = useUILocale();
  const selected = new Set(selection?.keys);
  const available = rows.filter(row => !selection?.isDisabled?.(row)).map(rowKey);
  const count = available.filter(key => selected.has(key)).length;
  function selectPage(checked: boolean) {
    for (const key of available) { if (checked) selected.add(key); else selected.delete(key); }
    selection?.onChange([...selected]);
  }
  return <div className={`ui-table-scroll ${className}`} tabIndex={0} role="region" aria-label={caption}>
    <table className="ui-table" aria-busy={loading || undefined}>
      <caption className="ui-sr-only">{caption}</caption>
      <thead><tr>
        {selection && <th scope="col" className="ui-table__select"><Checkbox label={messages.selectAll} hideLabel checked={count > 0 && count === available.length} indeterminate={count > 0 && count < available.length} disabled={loading || !available.length} onChange={event => selectPage(event.target.checked)} /></th>}
        {columns.map(column => <th scope="col" key={column.key} className={column.align === 'end' ? 'ui-table__end' : undefined} aria-sort={column.sortable && onSortChange ? sort?.column === column.key ? sort.direction === 'asc' ? 'ascending' : 'descending' : 'none' : undefined}>
          {column.sortable && onSortChange ? <button type="button" aria-label={messages.sort(column.header)} disabled={loading} onClick={() => onSortChange({ column: column.key, direction: sort?.column === column.key && sort.direction === 'asc' ? 'desc' : 'asc' })}>
            {column.header}<UIIcon name={sort?.column === column.key && sort.direction === 'desc' ? 'arrow-down' : 'arrow-up'} className={sort?.column !== column.key ? 'ui-table__unsorted' : undefined} />
          </button> : column.header}
        </th>)}
      </tr></thead>
      <tbody>{loading ? <tr><td colSpan={columns.length + (selection ? 1 : 0)}><Progress label={messages.loading} /></td></tr> : rows.length ? rows.map(row => {
        const key = rowKey(row);
        return <tr key={key} data-selected={selection && selected.has(key) || undefined}>
          {selection && <td className="ui-table__select"><Checkbox label={messages.selectRow(selection.rowLabel(row))} hideLabel checked={selected.has(key)} disabled={selection.isDisabled?.(row)} onChange={event => { if (event.target.checked) selected.add(key); else selected.delete(key); selection.onChange([...selected]); }} /></td>}
          {columns.map(column => <td key={column.key} className={column.align === 'end' ? 'ui-table__end' : undefined}>{column.cell(row)}</td>)}
        </tr>;
      }) : <tr><td colSpan={columns.length + (selection ? 1 : 0)}>{emptyState ?? '—'}</td></tr>}</tbody>
    </table>
  </div>;
}
