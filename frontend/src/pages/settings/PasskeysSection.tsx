import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Input, List, Popconfirm, Space, Typography, message } from 'antd';
import { DeleteOutlined, KeyOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';
import { createPasskey, passkeysSupported } from '@/utils/passkey';

interface Passkey {
  id: number;
  name: string;
  createdAt: number;
  lastUsedAt: number;
}

const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;

export default function PasskeysSection() {
  const { t } = useTranslation();
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  const [messageApi, messageContext] = message.useMessage();
  const supported = passkeysSupported();
  const { data = [], refetch } = useQuery({
    queryKey: ['passkeys', 'list'],
    queryFn: async () => {
      const msg = await HttpUtil.get<Passkey[]>('/panel/api/passkeys/list', undefined, {
        silent: true,
      });
      return msg?.success ? (msg.obj ?? []) : [];
    },
  });

  async function add() {
    setBusy(true);
    try {
      const begin = await HttpUtil.post<{ flow: string; options: Record<string, unknown> }>(
        '/panel/api/passkeys/register/begin',
        undefined,
        { silentSuccess: true },
      );
      if (!begin?.success || !begin.obj) return;
      let credential: Record<string, unknown>;
      try {
        credential = await createPasskey(begin.obj.options);
      } catch {
        messageApi.warning(t('pages.settings.passkeys.cancelled'));
        return;
      }
      const params = new URLSearchParams({ flow: begin.obj.flow, name });
      const msg = await HttpUtil.post(
        `/panel/api/passkeys/register/finish?${params.toString()}`,
        credential,
        JSON_HEADERS,
      );
      if (msg?.success) {
        setName('');
        void refetch();
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <div style={{ marginBottom: 24 }}>
      {messageContext}
      <Typography.Title level={5}>{t('pages.settings.passkeys.title')}</Typography.Title>
      <Typography.Paragraph type="secondary">
        {t('pages.settings.passkeys.intro')}
      </Typography.Paragraph>
      {!supported && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          title={t('pages.settings.passkeys.unsupported')}
        />
      )}
      <Space wrap style={{ marginBottom: 12 }}>
        <Input
          value={name}
          placeholder={t('pages.settings.passkeys.namePlaceholder')}
          onChange={(e) => setName(e.target.value)}
          style={{ width: 240 }}
        />
        <Button
          type="primary"
          icon={<KeyOutlined />}
          loading={busy}
          disabled={!supported}
          onClick={add}
        >
          {t('pages.settings.passkeys.add')}
        </Button>
      </Space>
      <List<Passkey>
        size="small"
        bordered
        dataSource={data}
        locale={{ emptyText: t('pages.settings.passkeys.empty') }}
        renderItem={(p) => (
          <List.Item
            actions={[
              <Popconfirm
                key="del"
                title={t('pages.settings.passkeys.deleteConfirm', { name: p.name })}
                onConfirm={async () => {
                  const msg = await HttpUtil.post(`/panel/api/passkeys/del/${p.id}`);
                  if (msg?.success) void refetch();
                }}
              >
                <Button
                  size="small"
                  type="text"
                  danger
                  icon={<DeleteOutlined />}
                  aria-label={t('delete')}
                />
              </Popconfirm>,
            ]}
          >
            <List.Item.Meta
              avatar={<KeyOutlined />}
              title={p.name}
              description={t('pages.settings.passkeys.meta', {
                created: new Date(p.createdAt * 1000).toLocaleDateString(),
                used: p.lastUsedAt ? new Date(p.lastUsedAt * 1000).toLocaleString() : '-',
              })}
            />
          </List.Item>
        )}
      />
    </div>
  );
}
