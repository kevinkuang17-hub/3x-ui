import { useEffect, useId, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  AutoComplete,
  Button,
  Card,
  ConfigProvider,
  Form,
  Input,
  InputNumber,
  Layout,
  Modal,
  Radio,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Tooltip,
  message,
} from 'antd';
import type { TableColumnsType } from 'antd';
import {
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
  ShoppingOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { useTheme } from '@/hooks/useTheme';
import { usePageTitle } from '@/hooks/usePageTitle';
import { HttpUtil } from '@/utils';
import { setMessageInstance } from '@/utils/messageBus';
import AppSidebar from '@/layouts/AppSidebar';
import { keys } from '@/api/queryKeys';

const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;
const GiB = 1024 ** 3;

export interface Plan {
  id: number;
  name: string;
  totalGB: number;
  days: number;
  limitIp: number;
  group: string;
  delayStart: boolean;
  resetTraffic: boolean;
  price: string;
  remark: string;
  sortOrder: number;
}

const EMPTY_PLAN: Plan = {
  id: 0,
  name: '',
  totalGB: 0,
  days: 30,
  limitIp: 0,
  group: '',
  delayStart: false,
  resetTraffic: true,
  price: '',
  remark: '',
  sortOrder: 0,
};

interface ApplyResult {
  applied: number;
  failed?: Record<string, string>;
}

async function fetchPlans(): Promise<Plan[]> {
  const msg = await HttpUtil.get<Plan[]>('/panel/api/plans/list', undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to load plans');
  return msg.obj ?? [];
}

async function fetchEmails(): Promise<string[]> {
  const msg = await HttpUtil.get<{ email: string }[]>('/panel/api/clients/list', undefined, {
    silent: true,
  });
  return (msg?.obj ?? []).map((c) => c.email);
}

async function fetchGroupNames(): Promise<string[]> {
  const msg = await HttpUtil.get<{ name: string }[]>('/panel/api/clients/groups', undefined, {
    silent: true,
  });
  return (msg?.obj ?? []).map((g) => g.name);
}

export default function PlansPage() {
  const uid = useId();
  usePageTitle();
  const { t } = useTranslation();
  const { isDark, isUltra, antdThemeConfig } = useTheme();
  const [modal, modalContextHolder] = Modal.useModal();
  const [messageApi, messageContextHolder] = message.useMessage();
  useEffect(() => {
    setMessageInstance(messageApi);
  }, [messageApi]);
  const queryClient = useQueryClient();

  const plansQuery = useQuery({ queryKey: keys.plans.list(), queryFn: fetchPlans });
  const plans = plansQuery.data ?? [];

  const [editing, setEditing] = useState<Plan | null>(null);
  const [applying, setApplying] = useState<Plan | null>(null);
  const [applyEmails, setApplyEmails] = useState<string[]>([]);
  const [applyMode, setApplyMode] = useState<'renew' | 'reset'>('renew');

  const groupsQuery = useQuery({
    queryKey: keys.clients.groups(),
    queryFn: fetchGroupNames,
    enabled: editing !== null || applying !== null,
  });
  const emailsQuery = useQuery({
    queryKey: keys.clients.all(),
    queryFn: fetchEmails,
    enabled: applying !== null,
  });

  const invalidate = () => queryClient.invalidateQueries({ queryKey: keys.plans.root() });

  const saveMut = useMutation({
    mutationFn: (p: Plan) =>
      p.id
        ? HttpUtil.post(`/panel/api/plans/update/${p.id}`, p, JSON_HEADERS)
        : HttpUtil.post('/panel/api/plans/add', p, JSON_HEADERS),
    onSuccess: (msg) => {
      if (msg?.success) invalidate();
    },
  });
  const applyMut = useMutation({
    mutationFn: (body: { planId: number; emails: string[]; mode: string }) =>
      HttpUtil.post<ApplyResult>('/panel/api/plans/apply', body, {
        ...JSON_HEADERS,
        silentSuccess: true,
      }),
    onSuccess: (msg) => {
      if (msg?.success) queryClient.invalidateQueries({ queryKey: keys.clients.root() });
    },
  });

  async function saveEditing() {
    if (!editing) return;
    if (!editing.name.trim()) {
      messageApi.error(t('pages.plans.nameRequired'));
      return;
    }
    const msg = await saveMut.mutateAsync(editing);
    if (msg?.success) setEditing(null);
  }

  async function addGroupMembers(group: string) {
    const msg = await HttpUtil.get<string[]>(
      `/panel/api/clients/groups/${encodeURIComponent(group)}/emails`,
      undefined,
      { silent: true },
    );
    const members = msg?.obj ?? [];
    setApplyEmails((prev) => Array.from(new Set([...prev, ...members])));
  }

  async function confirmApply() {
    if (!applying || applyEmails.length === 0) return;
    const msg = await applyMut.mutateAsync({
      planId: applying.id,
      emails: applyEmails,
      mode: applyMode,
    });
    if (!msg?.success) return;
    const res = msg.obj ?? { applied: 0 };
    const failed = Object.entries(res.failed ?? {});
    if (failed.length === 0) {
      messageApi.success(t('pages.plans.applied', { count: res.applied }));
    } else {
      messageApi.warning(
        `${t('pages.plans.appliedMixed', { ok: res.applied, failed: failed.length })} — ${failed[0][0]}: ${failed[0][1]}`,
      );
    }
    setApplying(null);
  }

  function onDelete(p: Plan) {
    modal.confirm({
      title: t('pages.plans.deleteConfirm', { name: p.name }),
      okText: t('delete'),
      okType: 'danger',
      cancelText: t('cancel'),
      onOk: async () => {
        const msg = await HttpUtil.post(`/panel/api/plans/del/${p.id}`);
        if (msg?.success) invalidate();
      },
    });
  }

  const columns: TableColumnsType<Plan> = [
    {
      title: t('pages.clients.actions'),
      key: 'actions',
      width: 130,
      render: (_v, row) => (
        <Space size={4}>
          <Tooltip title={t('pages.plans.apply')}>
            <Button
              aria-label={t('pages.plans.apply')}
              size="small"
              type="text"
              icon={<ThunderboltOutlined aria-hidden />}
              onClick={() => {
                setApplyEmails([]);
                setApplyMode('renew');
                setApplying(row);
              }}
            />
          </Tooltip>
          <Tooltip title={t('edit')}>
            <Button
              aria-label={t('edit')}
              size="small"
              type="text"
              icon={<EditOutlined aria-hidden />}
              onClick={() => setEditing({ ...row })}
            />
          </Tooltip>
          <Tooltip title={t('delete')}>
            <Button
              aria-label={t('delete')}
              size="small"
              type="text"
              danger
              icon={<DeleteOutlined aria-hidden />}
              onClick={() => onDelete(row)}
            />
          </Tooltip>
        </Space>
      ),
    },
    {
      title: t('pages.plans.name'),
      dataIndex: 'name',
      key: 'name',
      render: (name: string) => <Tag color="geekblue">{name}</Tag>,
    },
    {
      title: t('pages.plans.traffic'),
      dataIndex: 'totalGB',
      key: 'totalGB',
      render: (bytes: number) =>
        bytes > 0 ? `${+(bytes / GiB).toFixed(2)} GB` : t('pages.plans.unlimited'),
    },
    {
      title: t('pages.plans.days'),
      key: 'days',
      render: (_v, row) =>
        row.days > 0
          ? `${row.days}${row.delayStart ? ` (${t('pages.plans.delayStartShort')})` : ''}`
          : t('pages.plans.never'),
    },
    {
      title: t('pages.plans.limitIp'),
      dataIndex: 'limitIp',
      key: 'limitIp',
      render: (n: number) => (n > 0 ? n : t('pages.plans.unlimited')),
    },
    { title: t('pages.plans.group'), dataIndex: 'group', key: 'group' },
    { title: t('pages.plans.price'), dataIndex: 'price', key: 'price' },
  ];

  const pageClass = useMemo(() => {
    const classes = ['plans-page'];
    if (isDark) classes.push('is-dark');
    if (isUltra) classes.push('is-ultra');
    return classes.join(' ');
  }, [isDark, isUltra]);

  const groupOptions = (groupsQuery.data ?? []).map((g) => ({ value: g, label: g }));

  return (
    <ConfigProvider theme={antdThemeConfig}>
      {messageContextHolder}
      {modalContextHolder}
      <Layout className={pageClass}>
        <AppSidebar />
        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            <Card
              size="small"
              hoverable
              title={
                <div className="card-toolbar">
                  <Button
                    type="primary"
                    icon={<PlusOutlined aria-hidden />}
                    onClick={() => setEditing({ ...EMPTY_PLAN })}
                  >
                    {t('pages.plans.add')}
                  </Button>
                </div>
              }
            >
              <Table<Plan>
                dataSource={plans}
                columns={columns}
                rowKey="id"
                size="small"
                pagination={false}
                loading={plansQuery.isFetching}
                scroll={{ x: 'max-content' }}
                locale={{
                  emptyText: (
                    <div className="card-empty">
                      <ShoppingOutlined aria-hidden style={{ fontSize: 32, marginBottom: 8 }} />
                      <div>{t('pages.plans.empty')}</div>
                    </div>
                  ),
                }}
              />
            </Card>
          </Layout.Content>
        </Layout>

        <Modal
          open={editing !== null}
          title={editing?.id ? t('pages.plans.edit') : t('pages.plans.add')}
          okText={t('save')}
          cancelText={t('cancel')}
          confirmLoading={saveMut.isPending}
          onCancel={() => setEditing(null)}
          onOk={saveEditing}
          destroyOnHidden
        >
          {editing && (
            <Form layout="vertical">
              <Form.Item htmlFor={`${uid}-input-339`} label={t('pages.plans.name')} required>
                <Input
                  id={`${uid}-input-339`}
                  value={editing.name}
                  onChange={(e) => setEditing({ ...editing, name: e.target.value })}
                  autoFocus
                />
              </Form.Item>
              <Space wrap>
                <Form.Item
                  htmlFor={`${uid}-inputnumber-347`}
                  label={t('pages.plans.traffic')}
                  extra={t('pages.plans.zeroUnlimited')}
                >
                  <InputNumber
                    id={`${uid}-inputnumber-347`}
                    min={0}
                    addonAfter="GB"
                    value={+(editing.totalGB / GiB).toFixed(2)}
                    onChange={(v) =>
                      setEditing({ ...editing, totalGB: Math.round(Number(v ?? 0) * GiB) })
                    }
                  />
                </Form.Item>
                <Form.Item
                  htmlFor={`${uid}-inputnumber-357`}
                  label={t('pages.plans.days')}
                  extra={t('pages.plans.zeroNever')}
                >
                  <InputNumber
                    id={`${uid}-inputnumber-357`}
                    min={0}
                    value={editing.days}
                    onChange={(v) => setEditing({ ...editing, days: Number(v ?? 0) })}
                  />
                </Form.Item>
                <Form.Item
                  htmlFor={`${uid}-inputnumber-364`}
                  label={t('pages.plans.limitIp')}
                  extra={t('pages.plans.zeroUnlimited')}
                >
                  <InputNumber
                    id={`${uid}-inputnumber-364`}
                    min={0}
                    value={editing.limitIp}
                    onChange={(v) => setEditing({ ...editing, limitIp: Number(v ?? 0) })}
                  />
                </Form.Item>
              </Space>
              <Form.Item
                htmlFor={`${uid}-autocomplete-372`}
                label={t('pages.plans.group')}
                extra={t('pages.plans.groupDesc')}
              >
                <AutoComplete
                  id={`${uid}-autocomplete-372`}
                  value={editing.group}
                  options={groupOptions}
                  onChange={(v: string) => setEditing({ ...editing, group: v })}
                  allowClear
                />
              </Form.Item>
              <Form.Item
                htmlFor={`${uid}-switch-383`}
                label={t('pages.plans.delayStart')}
                extra={t('pages.plans.delayStartDesc')}
              >
                <Switch
                  id={`${uid}-switch-383`}
                  checked={editing.delayStart}
                  onChange={(v) => setEditing({ ...editing, delayStart: v })}
                />
              </Form.Item>
              <Form.Item
                htmlFor={`${uid}-switch-392`}
                label={t('pages.plans.resetTraffic')}
                extra={t('pages.plans.resetTrafficDesc')}
              >
                <Switch
                  id={`${uid}-switch-392`}
                  checked={editing.resetTraffic}
                  onChange={(v) => setEditing({ ...editing, resetTraffic: v })}
                />
              </Form.Item>
              <Space wrap>
                <Form.Item htmlFor={`${uid}-input-399`} label={t('pages.plans.price')}>
                  <Input
                    id={`${uid}-input-399`}
                    value={editing.price}
                    onChange={(e) => setEditing({ ...editing, price: e.target.value })}
                  />
                </Form.Item>
                <Form.Item htmlFor={`${uid}-inputnumber-405`} label={t('pages.plans.sortOrder')}>
                  <InputNumber
                    id={`${uid}-inputnumber-405`}
                    value={editing.sortOrder}
                    onChange={(v) => setEditing({ ...editing, sortOrder: Number(v ?? 0) })}
                  />
                </Form.Item>
              </Space>
              <Form.Item htmlFor={`${uid}-input-textarea-412`} label={t('pages.plans.remark')}>
                <Input.TextArea
                  id={`${uid}-input-textarea-412`}
                  value={editing.remark}
                  autoSize={{ minRows: 2, maxRows: 5 }}
                  onChange={(e) => setEditing({ ...editing, remark: e.target.value })}
                />
              </Form.Item>
            </Form>
          )}
        </Modal>

        <Modal
          open={applying !== null}
          title={t('pages.plans.applyTitle', { name: applying?.name ?? '' })}
          okText={t('pages.plans.apply')}
          okButtonProps={{ disabled: applyEmails.length === 0 }}
          cancelText={t('cancel')}
          confirmLoading={applyMut.isPending}
          onCancel={() => setApplying(null)}
          onOk={confirmApply}
          destroyOnHidden
        >
          <Form layout="vertical">
            <Form.Item htmlFor={`${uid}-select-435`} label={t('pages.plans.clients')}>
              <Select
                id={`${uid}-select-435`}
                mode="multiple"
                allowClear
                showSearch
                loading={emailsQuery.isFetching}
                value={applyEmails}
                onChange={setApplyEmails}
                options={(emailsQuery.data ?? []).map((e) => ({ value: e, label: e }))}
                style={{ width: '100%' }}
              />
            </Form.Item>
            <Form.Item htmlFor={`${uid}-select-447`} label={t('pages.plans.pickGroup')}>
              <Select
                id={`${uid}-select-447`}
                value={null}
                placeholder={t('pages.plans.pickGroupPlaceholder')}
                options={groupOptions}
                onChange={(g: string | null) => {
                  if (g) void addGroupMembers(g);
                }}
                style={{ width: '100%' }}
              />
            </Form.Item>
            <Form.Item label={t('pages.plans.mode')}>
              <Radio.Group value={applyMode} onChange={(e) => setApplyMode(e.target.value)}>
                <Radio value="renew">{t('pages.plans.modeRenew')}</Radio>
                <Radio value="reset">{t('pages.plans.modeReset')}</Radio>
              </Radio.Group>
            </Form.Item>
          </Form>
        </Modal>
      </Layout>
    </ConfigProvider>
  );
}
