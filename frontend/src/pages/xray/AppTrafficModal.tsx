import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Checkbox, Divider, Modal, Progress, Space, Table, Typography } from 'antd';
import { useQuery } from '@tanstack/react-query';

import { HttpUtil, SizeFormatter } from '@/utils';

interface AppTraffic {
  app: string;
  up: number;
  down: number;
}
interface Status {
  catalog: string[];
  enabled: string[];
  traffic: AppTraffic[];
}

interface Props {
  open: boolean;
  onClose: () => void;
  onChanged: () => void;
}

const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;

export default function AppTrafficModal({ open, onClose, onChanged }: Props) {
  const { t } = useTranslation();
  const [selection, setSelection] = useState<string[] | null>(null);
  const [saving, setSaving] = useState(false);
  const { data, refetch, isFetching } = useQuery({
    queryKey: ['apps', 'status'],
    queryFn: async () => {
      const msg = await HttpUtil.get<Status>('/panel/api/apps/status', undefined, { silent: true });
      return msg?.success && msg.obj ? msg.obj : { catalog: [], enabled: [], traffic: [] };
    },
    enabled: open,
    gcTime: 0,
  });
  const selected = selection ?? data?.enabled ?? [];
  const traffic = data?.traffic ?? [];
  const max = Math.max(1, ...traffic.map((r) => r.up + r.down));

  async function save() {
    setSaving(true);
    try {
      const msg = await HttpUtil.post('/panel/api/apps/set', { apps: selected }, JSON_HEADERS);
      if (msg?.success) {
        setSelection(null);
        void refetch();
        onChanged();
      }
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      open={open}
      title={t('pages.xray.apps.title')}
      onCancel={onClose}
      width={680}
      destroyOnHidden
      footer={
        <Button type="primary" loading={saving} onClick={save}>
          {t('save')}
        </Button>
      }
    >
      <Alert type="info" showIcon style={{ marginBottom: 12 }} title={t('pages.xray.apps.intro')} />
      <Typography.Text strong>{t('pages.xray.apps.track')}</Typography.Text>
      <Checkbox.Group
        style={{ display: 'flex', flexWrap: 'wrap', gap: 8, marginTop: 8 }}
        value={selected}
        onChange={(v) => setSelection(v as string[])}
        options={(data?.catalog ?? []).map((a) => ({ value: a, label: a }))}
      />
      <Divider />
      <Table<AppTraffic>
        rowKey="app"
        size="small"
        loading={isFetching}
        dataSource={traffic}
        pagination={false}
        locale={{ emptyText: t('pages.xray.apps.empty') }}
        columns={[
          { title: t('pages.xray.apps.app'), dataIndex: 'app', width: 120 },
          {
            title: t('pages.xray.apps.usage'),
            key: 'usage',
            render: (_v, r) => (
              <Space orientation="vertical" size={0} style={{ width: '100%' }}>
                <Progress percent={Math.round(((r.up + r.down) * 100) / max)} showInfo={false} />
                <Typography.Text type="secondary">
                  ↑ {SizeFormatter.sizeFormat(r.up)} · ↓ {SizeFormatter.sizeFormat(r.down)}
                </Typography.Text>
              </Space>
            ),
          },
        ]}
      />
    </Modal>
  );
}
