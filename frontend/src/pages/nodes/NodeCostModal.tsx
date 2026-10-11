import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, DatePicker, Form, Input, InputNumber, Modal, Space } from 'antd';
import dayjs from 'dayjs';

import { HttpUtil } from '@/utils';
import type { NodeRecord } from '@/api/queries/useNodesQuery';

export interface NodeCost {
  nodeId: number;
  provider: string;
  monthly: number;
  currency: string;
  renewAt: number;
  note: string;
}

interface Props {
  node: NodeRecord;
  cost: NodeCost | undefined;
  onClose: () => void;
  onSaved: () => void;
}

const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;

export default function NodeCostModal({ node, cost, onClose, onSaved }: Props) {
  const { t } = useTranslation();
  const [draft, setDraft] = useState<NodeCost>(
    cost ?? { nodeId: node.id, provider: '', monthly: 0, currency: 'USD', renewAt: 0, note: '' },
  );
  const [busy, setBusy] = useState(false);

  async function run(fn: () => Promise<{ success?: boolean } | undefined>) {
    setBusy(true);
    try {
      const msg = await fn();
      if (msg?.success) {
        onSaved();
        onClose();
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      open
      title={t('pages.nodes.cost.modalTitle', { name: node.name ?? '' })}
      onCancel={onClose}
      destroyOnHidden
      footer={
        <Space>
          {cost && (
            <Button
              danger
              loading={busy}
              onClick={() => run(() => HttpUtil.post(`/panel/api/nodes/cost/del/${node.id}`))}
            >
              {t('delete')}
            </Button>
          )}
          <Button
            type="primary"
            loading={busy}
            onClick={() =>
              run(() => HttpUtil.post('/panel/api/nodes/cost/set', draft, JSON_HEADERS))
            }
          >
            {t('save')}
          </Button>
        </Space>
      }
    >
      <Form layout="vertical">
        <Form.Item label={t('pages.nodes.cost.provider')}>
          <Input
            value={draft.provider}
            placeholder="Vultr / BandwagonHost / …"
            onChange={(e) => setDraft({ ...draft, provider: e.target.value })}
          />
        </Form.Item>
        <Space wrap>
          <Form.Item label={t('pages.nodes.cost.monthly')}>
            <InputNumber
              min={0}
              step={0.5}
              value={draft.monthly}
              onChange={(v) => setDraft({ ...draft, monthly: Number(v ?? 0) })}
            />
          </Form.Item>
          <Form.Item label={t('pages.nodes.cost.currency')}>
            <Input
              style={{ width: 100 }}
              value={draft.currency}
              maxLength={8}
              onChange={(e) => setDraft({ ...draft, currency: e.target.value.toUpperCase() })}
            />
          </Form.Item>
          <Form.Item label={t('pages.nodes.cost.renewAt')}>
            <DatePicker
              value={draft.renewAt ? dayjs.unix(draft.renewAt) : null}
              onChange={(d) => setDraft({ ...draft, renewAt: d ? d.endOf('day').unix() : 0 })}
            />
          </Form.Item>
        </Space>
        <Form.Item label={t('pages.plans.remark')}>
          <Input.TextArea
            autoSize={{ minRows: 2, maxRows: 4 }}
            value={draft.note}
            onChange={(e) => setDraft({ ...draft, note: e.target.value })}
          />
        </Form.Item>
      </Form>
    </Modal>
  );
}
