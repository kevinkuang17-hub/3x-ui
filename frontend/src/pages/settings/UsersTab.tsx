import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd';
import type { TableColumnsType } from 'antd';
import { DeleteOutlined, EditOutlined, KeyOutlined, PlusOutlined } from '@ant-design/icons';
import { useQuery } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';
import PasskeysSection from './PasskeysSection';

interface PanelUser {
  id: number;
  username: string;
  role: 'admin' | 'operator';
  primary: boolean;
  passkeys: number;
}

const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;

export default function UsersTab() {
  const uid = useId();
  const { t } = useTranslation();
  const [editing, setEditing] = useState<{
    id: number;
    username: string;
    password: string;
    role: string;
  } | null>(null);
  const [busy, setBusy] = useState(false);
  const {
    data = [],
    refetch,
    isFetching,
  } = useQuery({
    queryKey: ['users', 'list'],
    queryFn: async () => {
      const msg = await HttpUtil.get<PanelUser[]>('/panel/api/users/list', undefined, {
        silent: true,
      });
      return msg?.success ? (msg.obj ?? []) : [];
    },
  });

  async function save() {
    if (!editing) return;
    setBusy(true);
    try {
      const msg = editing.id
        ? await HttpUtil.post(
            `/panel/api/users/update/${editing.id}`,
            { password: editing.password, role: editing.role },
            JSON_HEADERS,
          )
        : await HttpUtil.post('/panel/api/users/add', editing, JSON_HEADERS);
      if (msg?.success) {
        setEditing(null);
        void refetch();
      }
    } finally {
      setBusy(false);
    }
  }

  const columns: TableColumnsType<PanelUser> = [
    { title: t('pages.settings.users.username'), dataIndex: 'username' },
    {
      title: t('pages.settings.users.role'),
      dataIndex: 'role',
      render: (role: string, row) => (
        <Space size={4}>
          <Tag color={role === 'admin' ? 'gold' : 'blue'}>
            {t(`pages.settings.users.role_${role}`)}
          </Tag>
          {row.primary && <Tag>{t('pages.settings.users.primary')}</Tag>}
          {row.passkeys > 0 && <Tag icon={<KeyOutlined aria-hidden />}>{row.passkeys}</Tag>}
        </Space>
      ),
    },
    {
      key: 'actions',
      width: 100,
      render: (_v, row) =>
        row.primary ? null : (
          <Space size={4}>
            <Button
              size="small"
              type="text"
              icon={<EditOutlined aria-hidden />}
              aria-label={t('edit')}
              onClick={() =>
                setEditing({ id: row.id, username: row.username, password: '', role: row.role })
              }
            />
            {row.passkeys > 0 && (
              <Popconfirm
                title={t('pages.settings.users.clearPasskeysConfirm', { name: row.username })}
                onConfirm={async () => {
                  const msg = await HttpUtil.post(`/panel/api/users/clearPasskeys/${row.id}`);
                  if (msg?.success) void refetch();
                }}
              >
                <Button
                  size="small"
                  type="text"
                  icon={<KeyOutlined aria-hidden />}
                  aria-label={t('pages.settings.users.clearPasskeys')}
                  title={t('pages.settings.users.clearPasskeys')}
                />
              </Popconfirm>
            )}
            <Popconfirm
              title={t('pages.settings.users.deleteConfirm', { name: row.username })}
              onConfirm={async () => {
                const msg = await HttpUtil.post(`/panel/api/users/del/${row.id}`);
                if (msg?.success) void refetch();
              }}
            >
              <Button
                size="small"
                type="text"
                danger
                icon={<DeleteOutlined aria-hidden />}
                aria-label={t('delete')}
              />
            </Popconfirm>
          </Space>
        ),
    },
  ];

  return (
    <>
      <PasskeysSection />
      <Typography.Title level={5}>{t('pages.settings.users.title')}</Typography.Title>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        title={t('pages.settings.users.intro')}
      />
      <Button
        type="primary"
        icon={<PlusOutlined aria-hidden />}
        style={{ marginBottom: 12 }}
        onClick={() => setEditing({ id: 0, username: '', password: '', role: 'operator' })}
      >
        {t('pages.settings.users.add')}
      </Button>
      <Table<PanelUser>
        rowKey="id"
        size="small"
        loading={isFetching}
        dataSource={data}
        columns={columns}
        pagination={false}
      />
      <Modal
        open={editing !== null}
        title={editing?.id ? editing.username : t('pages.settings.users.add')}
        okText={t('save')}
        cancelText={t('cancel')}
        confirmLoading={busy}
        onCancel={() => setEditing(null)}
        onOk={save}
        destroyOnHidden
      >
        {editing && (
          <Form layout="vertical">
            {!editing.id && (
              <Form.Item htmlFor={`${uid}-input-183`} label={t('pages.settings.users.username')}>
                <Input
                  id={`${uid}-input-183`}
                  value={editing.username}
                  autoComplete="off"
                  onChange={(e) => setEditing({ ...editing, username: e.target.value })}
                />
              </Form.Item>
            )}
            <Form.Item
              htmlFor={`${uid}-input-password-194`}
              label={t('pages.settings.users.password')}
              extra={editing.id ? t('pages.settings.users.passwordKeep') : undefined}
            >
              <Input.Password
                id={`${uid}-input-password-194`}
                value={editing.password}
                autoComplete="new-password"
                onChange={(e) => setEditing({ ...editing, password: e.target.value })}
              />
            </Form.Item>
            <Form.Item
              htmlFor={`${uid}-select-204`}
              label={t('pages.settings.users.role')}
              extra={t(`pages.settings.users.roleDesc_${editing.role}`)}
            >
              <Select
                id={`${uid}-select-204`}
                value={editing.role}
                onChange={(role) => setEditing({ ...editing, role })}
                options={['operator', 'admin'].map((r) => ({
                  value: r,
                  label: t(`pages.settings.users.role_${r}`),
                }))}
              />
            </Form.Item>
          </Form>
        )}
      </Modal>
    </>
  );
}
