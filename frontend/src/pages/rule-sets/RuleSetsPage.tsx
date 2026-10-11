import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  AutoComplete,
  Button,
  Card,
  ConfigProvider,
  Form,
  Input,
  Layout,
  Modal,
  Select,
  Space,
  Table,
  Tabs,
  Tag,
  Tooltip,
  message,
} from 'antd';
import type { TableColumnsType } from 'antd';
import {
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
  RollbackOutlined,
  SaveOutlined,
} from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { useTheme } from '@/hooks/useTheme';
import { usePageTitle } from '@/hooks/usePageTitle';
import { HttpUtil } from '@/utils';
import { setMessageInstance } from '@/utils/messageBus';
import AppSidebar from '@/layouts/AppSidebar';
import { keys } from '@/api/queryKeys';
import RoutingChoiceEditor from './RoutingChoiceEditor';
import TemplatePicker, { useTemplateGroups } from './TemplatePicker';
import {
  JSON_HEADERS,
  ROUTING_POLICIES,
  type GlobalRouting,
  type RoutingChoice,
  type RuleSet,
  fetchGlobalRouting,
  fetchRuleSets,
  ruleCount,
} from './ruleSetApi';

const EMPTY_SET: RuleSet = {
  id: 0,
  name: '',
  category: '',
  rules: '',
  defaultPolicy: 'PROXY',
  builtinKey: '',
  edited: false,
  sortOrder: 0,
};

function GlobalRoutingTab({ sets, initial }: { sets: RuleSet[]; initial: GlobalRouting }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [choice, setChoice] = useState<RoutingChoice>({
    ruleSets: initial.ruleSets,
    extraRules: initial.extraRules,
  });
  const [template, setTemplate] = useState(initial.templateOn ? initial.template : '');
  const groupsQuery = useTemplateGroups(template);
  const saveMut = useMutation({
    mutationFn: (body: RoutingChoice & { template: string }) =>
      HttpUtil.post('/panel/api/ruleSets/routing', body, JSON_HEADERS),
    onSuccess: (msg) => {
      if (msg?.success) queryClient.invalidateQueries({ queryKey: keys.ruleSets.routing() });
    },
  });
  return (
    <>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 16 }}
        title={t('pages.ruleSets.globalDesc')}
      />
      {!initial.clashEnabled && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 16 }}
          title={t('pages.ruleSets.clashOffNote')}
        />
      )}
      <TemplatePicker
        value={template}
        onChange={setTemplate}
        groups={groupsQuery.data ?? []}
        groupsError={
          groupsQuery.error
            ? t('pages.ruleSets.templateGroupsFailed', { msg: groupsQuery.error.message })
            : undefined
        }
      />
      <RoutingChoiceEditor
        sets={sets}
        value={choice}
        onChange={setChoice}
        groups={groupsQuery.data ?? []}
      />
      <div style={{ marginTop: 16 }}>
        <Button
          type="primary"
          icon={<SaveOutlined />}
          loading={saveMut.isPending}
          onClick={() => saveMut.mutate({ ...choice, template })}
        >
          {t('save')}
        </Button>
      </div>
    </>
  );
}

export default function RuleSetsPage() {
  usePageTitle();
  const { t } = useTranslation();
  const { isDark, isUltra, antdThemeConfig } = useTheme();
  const [modal, modalContextHolder] = Modal.useModal();
  const [messageApi, messageContextHolder] = message.useMessage();
  useEffect(() => {
    setMessageInstance(messageApi);
  }, [messageApi]);
  const queryClient = useQueryClient();

  const setsQuery = useQuery({ queryKey: keys.ruleSets.list(), queryFn: fetchRuleSets });
  const routingQuery = useQuery({
    queryKey: keys.ruleSets.routing(),
    queryFn: fetchGlobalRouting,
  });
  const sets = setsQuery.data ?? [];
  const [editing, setEditing] = useState<RuleSet | null>(null);

  const invalidate = () => queryClient.invalidateQueries({ queryKey: keys.ruleSets.list() });
  const saveMut = useMutation({
    mutationFn: (rs: RuleSet) =>
      HttpUtil.post<RuleSet>('/panel/api/ruleSets/save', rs, JSON_HEADERS),
    onSuccess: (msg) => {
      if (msg?.success) invalidate();
    },
  });

  async function saveEditing() {
    if (!editing) return;
    const msg = await saveMut.mutateAsync(editing);
    if (msg?.success) setEditing(null);
  }

  function onDelete(rs: RuleSet) {
    modal.confirm({
      title: t('pages.ruleSets.deleteConfirm', { name: rs.name }),
      okText: t('delete'),
      okType: 'danger',
      cancelText: t('cancel'),
      onOk: async () => {
        const msg = await HttpUtil.post(`/panel/api/ruleSets/del/${rs.id}`);
        if (msg?.success) invalidate();
      },
    });
  }

  function onReset(rs: RuleSet) {
    modal.confirm({
      title: t('pages.ruleSets.resetConfirm', { name: rs.name }),
      okText: t('confirm'),
      cancelText: t('cancel'),
      onOk: async () => {
        const msg = await HttpUtil.post(`/panel/api/ruleSets/reset/${rs.id}`);
        if (msg?.success) invalidate();
      },
    });
  }

  const categories = Array.from(new Set(sets.map((s) => s.category).filter(Boolean))).map((c) => ({
    value: c,
  }));

  const columns: TableColumnsType<RuleSet> = [
    {
      title: t('pages.clients.actions'),
      key: 'actions',
      width: 110,
      render: (_v, row) => (
        <Space size={4}>
          <Tooltip title={t('edit')}>
            <Button
              aria-label={t('edit')}
              size="small"
              type="text"
              icon={<EditOutlined />}
              onClick={() => setEditing({ ...row })}
            />
          </Tooltip>
          {row.builtinKey ? (
            <Tooltip title={t('pages.ruleSets.reset')}>
              <Button
                aria-label={t('pages.ruleSets.reset')}
                size="small"
                type="text"
                disabled={!row.edited}
                icon={<RollbackOutlined />}
                onClick={() => onReset(row)}
              />
            </Tooltip>
          ) : (
            <Tooltip title={t('delete')}>
              <Button
                aria-label={t('delete')}
                size="small"
                type="text"
                danger
                icon={<DeleteOutlined />}
                onClick={() => onDelete(row)}
              />
            </Tooltip>
          )}
        </Space>
      ),
    },
    {
      title: t('pages.ruleSets.name'),
      key: 'name',
      render: (_v, row) => (
        <Space size={6} wrap>
          <span>{row.name}</span>
          {row.builtinKey && (
            <Tag bordered={false} color={row.edited ? 'orange' : 'default'}>
              {row.edited ? t('pages.ruleSets.builtinEdited') : t('pages.ruleSets.builtin')}
            </Tag>
          )}
        </Space>
      ),
    },
    { title: t('pages.ruleSets.category'), dataIndex: 'category', key: 'category' },
    {
      title: t('pages.ruleSets.count'),
      key: 'count',
      render: (_v, row) => ruleCount(row.rules),
    },
    {
      title: t('pages.ruleSets.defaultPolicy'),
      key: 'defaultPolicy',
      render: (_v, row) => t(`pages.ruleSets.policy_${row.defaultPolicy}`),
    },
  ];

  const pageClass = useMemo(() => {
    const classes = ['rule-sets-page'];
    if (isDark) classes.push('is-dark');
    if (isUltra) classes.push('is-ultra');
    return classes.join(' ');
  }, [isDark, isUltra]);

  return (
    <ConfigProvider theme={antdThemeConfig}>
      {messageContextHolder}
      {modalContextHolder}
      <Layout className={pageClass}>
        <AppSidebar />
        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            <Card size="small">
              <Tabs
                items={[
                  {
                    key: 'global',
                    label: t('pages.ruleSets.globalTab'),
                    children:
                      routingQuery.data && setsQuery.data ? (
                        <GlobalRoutingTab
                          key={routingQuery.dataUpdatedAt}
                          sets={sets}
                          initial={routingQuery.data}
                        />
                      ) : null,
                  },
                  {
                    key: 'library',
                    label: t('pages.ruleSets.libraryTab'),
                    children: (
                      <>
                        <div className="card-toolbar" style={{ marginBottom: 12 }}>
                          <Button
                            type="primary"
                            icon={<PlusOutlined />}
                            onClick={() => setEditing({ ...EMPTY_SET })}
                          >
                            {t('pages.ruleSets.add')}
                          </Button>
                        </div>
                        <Table<RuleSet>
                          dataSource={sets}
                          columns={columns}
                          rowKey="id"
                          size="small"
                          pagination={false}
                          loading={setsQuery.isFetching}
                          scroll={{ x: 'max-content' }}
                        />
                      </>
                    ),
                  },
                ]}
              />
            </Card>
          </Layout.Content>
        </Layout>

        <Modal
          open={editing !== null}
          title={editing?.id ? t('pages.ruleSets.edit') : t('pages.ruleSets.add')}
          okText={t('save')}
          cancelText={t('cancel')}
          confirmLoading={saveMut.isPending}
          onCancel={() => setEditing(null)}
          onOk={saveEditing}
          width={640}
          destroyOnHidden
        >
          {editing && (
            <Form layout="vertical">
              {editing.builtinKey && (
                <Alert
                  type="info"
                  showIcon
                  style={{ marginBottom: 12 }}
                  title={t('pages.ruleSets.builtinEditNote')}
                />
              )}
              <Form.Item label={t('pages.ruleSets.name')} required>
                <Input
                  value={editing.name}
                  onChange={(e) => setEditing({ ...editing, name: e.target.value })}
                  autoFocus
                />
              </Form.Item>
              <Space wrap>
                <Form.Item label={t('pages.ruleSets.category')}>
                  <AutoComplete
                    style={{ width: 200 }}
                    value={editing.category}
                    options={categories}
                    onChange={(v: string) => setEditing({ ...editing, category: v })}
                  />
                </Form.Item>
                <Form.Item label={t('pages.ruleSets.defaultPolicy')}>
                  <Select
                    style={{ width: 160 }}
                    value={editing.defaultPolicy}
                    options={ROUTING_POLICIES.map((p) => ({
                      value: p,
                      label: t(`pages.ruleSets.policy_${p}`),
                    }))}
                    onChange={(p) => setEditing({ ...editing, defaultPolicy: p })}
                  />
                </Form.Item>
              </Space>
              <Form.Item
                label={t('pages.ruleSets.rules')}
                extra={t('pages.ruleSets.rulesDesc')}
                required
              >
                <Input.TextArea
                  value={editing.rules}
                  onChange={(e) => setEditing({ ...editing, rules: e.target.value })}
                  autoSize={{ minRows: 8, maxRows: 20 }}
                  placeholder={'maersk.com\nmsc.com\nDOMAIN-KEYWORD,cosco\nIP-CIDR,198.51.100.0/24'}
                  style={{ fontFamily: 'monospace' }}
                />
              </Form.Item>
            </Form>
          )}
        </Modal>
      </Layout>
    </ConfigProvider>
  );
}
