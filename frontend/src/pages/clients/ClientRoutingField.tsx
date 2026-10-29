import { lazy, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Form, Space, Tag, Typography } from 'antd';
import { BranchesOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';

import { LazyMount } from '@/components/utility';
import { clientRoutingKey, fetchClientRouting, type RoutingSource } from './clientRoutingApi';

const ClientRoutingModal = lazy(() => import('./ClientRoutingModal'));

const SOURCE_COLOR: Record<RoutingSource, string> = {
  client: 'purple',
  group: 'geekblue',
  global: 'default',
};

// Shows whose routing the client's subscription uses and opens the editor for its own.
export default function ClientRoutingField({ email }: { email: string }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const routing = useQuery({
    queryKey: clientRoutingKey(email),
    queryFn: () => fetchClientRouting(email),
    enabled: !!email,
    retry: false,
  });
  const data = routing.data;
  const label = !data
    ? ''
    : data.source === 'client'
      ? t('pages.clients.routingSourceClient')
      : data.source === 'group'
        ? t('pages.clients.routingSourceGroup', { group: data.group })
        : t('pages.clients.routingSourceGlobal');

  return (
    <Form.Item label={t('pages.clients.routing')}>
      <Space wrap>
        {data && <Tag color={SOURCE_COLOR[data.source]}>{label}</Tag>}
        {routing.error && (
          <Typography.Text type="secondary">{routing.error.message}</Typography.Text>
        )}
        <Button
          size="small"
          icon={<BranchesOutlined aria-hidden />}
          disabled={!data}
          onClick={() => setOpen(true)}
        >
          {t('pages.clients.routingEdit')}
        </Button>
      </Space>
      <LazyMount when={open}>
        <ClientRoutingModal open={open} email={email} onClose={() => setOpen(false)} />
      </LazyMount>
    </Form.Item>
  );
}
