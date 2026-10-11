import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Form, Input, Modal, Select, Switch, Table, Tag, Typography } from 'antd';
import type { TableColumnsType } from 'antd';
import { EyeOutlined } from '@ant-design/icons';

import { HttpUtil } from '@/utils';
import {
  JSON_HEADERS,
  ROUTING_POLICIES,
  type RoutingChoice,
  type RoutingPolicy,
  type RuleSet,
  ruleCount,
} from './ruleSetApi';

interface Props {
  sets: RuleSet[];
  loading?: boolean;
  value: RoutingChoice;
  onChange: (next: RoutingChoice) => void;
}

const POLICY_COLORS: Record<RoutingPolicy, string> = {
  DIRECT: 'green',
  PROXY: 'blue',
  REJECT: 'red',
};

export default function RoutingChoiceEditor({ sets, loading, value, onChange }: Props) {
  const { t } = useTranslation();
  const [preview, setPreview] = useState<string[] | null>(null);
  const [previewing, setPreviewing] = useState(false);
  const picked = new Map(value.ruleSets.map((p) => [p.id, p.policy]));

  // Picks follow the library order, which is also the order their rules are matched in.
  function setPick(set: RuleSet, policy: RoutingPolicy | null) {
    const next = new Map(picked);
    if (policy) next.set(set.id, policy);
    else next.delete(set.id);
    onChange({
      ...value,
      ruleSets: sets
        .filter((s) => next.has(s.id))
        .map((s) => ({ id: s.id, policy: next.get(s.id) as RoutingPolicy })),
    });
  }

  async function showPreview() {
    setPreviewing(true);
    try {
      const msg = await HttpUtil.post<{ rules: string[] }>('/panel/api/ruleSets/preview', value, {
        ...JSON_HEADERS,
        silentSuccess: true,
      });
      if (msg?.success) setPreview(msg.obj?.rules ?? []);
    } finally {
      setPreviewing(false);
    }
  }

  const policyOptions = ROUTING_POLICIES.map((p) => ({
    value: p,
    label: t(`pages.ruleSets.policy_${p}`),
  }));

  const columns: TableColumnsType<RuleSet> = [
    {
      title: t('pages.ruleSets.use'),
      key: 'use',
      width: 64,
      render: (_v, row) => (
        <Switch
          size="small"
          checked={picked.has(row.id)}
          onChange={(on) => setPick(row, on ? row.defaultPolicy : null)}
        />
      ),
    },
    {
      title: t('pages.ruleSets.name'),
      key: 'name',
      render: (_v, row) => (
        <span>
          {row.name}
          {row.category && (
            <Tag style={{ marginInlineStart: 8 }} bordered={false}>
              {row.category}
            </Tag>
          )}
        </span>
      ),
    },
    {
      title: t('pages.ruleSets.count'),
      key: 'count',
      width: 80,
      render: (_v, row) => ruleCount(row.rules),
    },
    {
      title: t('pages.ruleSets.policy'),
      key: 'policy',
      width: 140,
      render: (_v, row) => {
        const policy = picked.get(row.id);
        return policy ? (
          <Select
            size="small"
            style={{ width: 120 }}
            value={policy}
            options={policyOptions}
            onChange={(p: RoutingPolicy) => setPick(row, p)}
          />
        ) : (
          <Tag bordered={false} color={POLICY_COLORS[row.defaultPolicy]} style={{ opacity: 0.5 }}>
            {t(`pages.ruleSets.policy_${row.defaultPolicy}`)}
          </Tag>
        );
      },
    },
  ];

  return (
    <Form layout="vertical">
      <Form.Item label={t('pages.ruleSets.pickTitle')} extra={t('pages.ruleSets.pickDesc')}>
        <Table<RuleSet>
          dataSource={sets}
          columns={columns}
          rowKey="id"
          size="small"
          pagination={false}
          loading={loading}
          scroll={{ x: 'max-content', y: 360 }}
        />
      </Form.Item>
      <Form.Item label={t('pages.ruleSets.extraTitle')} extra={t('pages.ruleSets.extraDesc')}>
        <Input.TextArea
          value={value.extraRules}
          onChange={(e) => onChange({ ...value, extraRules: e.target.value })}
          autoSize={{ minRows: 4, maxRows: 14 }}
          placeholder={
            'example.com\nbank.example,DIRECT\n203.0.113.0/24,REJECT\nDOMAIN-KEYWORD,teams,PROXY'
          }
          style={{ fontFamily: 'monospace' }}
        />
      </Form.Item>
      <Button icon={<EyeOutlined />} loading={previewing} onClick={showPreview}>
        {t('pages.ruleSets.preview')}
      </Button>
      <Modal
        open={preview !== null}
        title={t('pages.ruleSets.previewTitle', { count: preview?.length ?? 0 })}
        footer={null}
        onCancel={() => setPreview(null)}
        width={720}
      >
        <Typography.Paragraph type="secondary">
          {t('pages.ruleSets.previewDesc')}
        </Typography.Paragraph>
        <pre style={{ maxHeight: 420, overflow: 'auto', fontSize: 12, margin: 0 }}>
          {(preview ?? []).join('\n') || t('pages.ruleSets.previewEmpty')}
        </pre>
      </Modal>
    </Form>
  );
}
