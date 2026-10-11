import { useTranslation } from 'react-i18next';
import { Alert, Space, Tag } from 'antd';
import dayjs from 'dayjs';

import type { NodeRecord } from '@/api/queries/useNodesQuery';
import type { NodeCost } from './NodeCostModal';

const SOON_DAYS = 7;

export default function NodeCostSummary({
  costs,
  nodes,
}: {
  costs: NodeCost[];
  nodes: NodeRecord[];
}) {
  const { t } = useTranslation();
  const totals = new Map<string, number>();
  for (const c of costs) totals.set(c.currency, (totals.get(c.currency) ?? 0) + c.monthly);
  const names = new Map(nodes.map((n) => [n.id, n.name ?? `#${n.id}`]));
  const now = dayjs();
  const soon = costs
    .filter((c) => c.renewAt > 0 && dayjs.unix(c.renewAt).diff(now, 'day') < SOON_DAYS)
    .sort((a, b) => a.renewAt - b.renewAt);

  return (
    <Alert
      type={soon.length > 0 ? 'warning' : 'info'}
      showIcon
      title={
        <Space wrap size={[12, 4]}>
          <span>
            {t('pages.nodes.cost.total')}:{' '}
            {[...totals.entries()].map(([cur, sum]) => `${sum.toFixed(2)} ${cur}`).join(' + ')}
          </span>
          {soon.length > 0 && <span>{t('pages.nodes.cost.renewSoon')}:</span>}
          {soon.map((c) => (
            <Tag key={c.nodeId} color={dayjs.unix(c.renewAt).isBefore(now) ? 'red' : 'orange'}>
              {names.get(c.nodeId) ?? c.nodeId} · {dayjs.unix(c.renewAt).format('YYYY-MM-DD')}
            </Tag>
          ))}
        </Space>
      }
    />
  );
}
