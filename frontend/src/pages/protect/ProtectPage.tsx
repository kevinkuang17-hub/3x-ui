import { useEffect, useId, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Card,
  ConfigProvider,
  Descriptions,
  Form,
  Input,
  Layout,
  Modal,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
  message,
} from 'antd';
import type { TableColumnsType } from 'antd';
import {
  EyeInvisibleOutlined,
  LockOutlined,
  ReloadOutlined,
  SaveOutlined,
  ThunderboltOutlined,
  UnlockOutlined,
} from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { useTheme } from '@/hooks/useTheme';
import { usePageTitle } from '@/hooks/usePageTitle';
import { HttpUtil } from '@/utils';
import { setMessageInstance } from '@/utils/messageBus';
import AppSidebar from '@/layouts/AppSidebar';
import { keys } from '@/api/queryKeys';
import type { CfOnlyStatus } from '@/generated/types';
import FirewallCard from './FirewallCard';
import RealitySelfCard from './RealitySelfCard';
import DecoyCard from './DecoyCard';

const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;
const API = '/panel/api/protect';

interface SpeedupStatus {
  kernel: string;
  congestionControl: string;
  qdisc: string;
  available: string[];
  fastOpen: string;
  rmemMax: string;
  applied: boolean;
  bbrOn: boolean;
}

interface CfInfo {
  panelDomain: string;
  panelPort: number;
  subDomain: string;
  subPort: number;
  subEnable: boolean;
  cfConfigured: boolean;
}

interface CfStatus {
  domain: string;
  found: boolean;
  ip: string;
  proxied: boolean;
  port: number;
  portOk: boolean;
  sslMode: string;
}

interface NodeRow {
  id: number;
  name: string;
  address: string;
  online: boolean;
  speedup?: SpeedupStatus;
  allowIps?: string;
  error?: string;
}

async function getObj<T>(path: string): Promise<T> {
  const msg = await HttpUtil.get<T>(`${API}${path}`, undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || 'request failed');
  return msg.obj as T;
}

function isIP(host: string): boolean {
  return /^[\d.]+$/.test(host) || host.includes(':');
}

function AccessCard() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: [...keys.protect.root(), 'access'],
    queryFn: () => getObj<{ allowIps: string; callerIp: string }>('/access'),
  });
  return (
    <Card size="small" title={t('pages.protect.accessTitle')}>
      <Typography.Paragraph type="secondary">{t('pages.protect.accessDesc')}</Typography.Paragraph>
      {query.data && (
        <AccessForm
          key={query.dataUpdatedAt}
          initial={query.data.allowIps}
          callerIp={query.data.callerIp}
          onChanged={() =>
            queryClient.invalidateQueries({ queryKey: [...keys.protect.root(), 'access'] })
          }
        />
      )}
      <Alert
        type="info"
        showIcon
        style={{ marginTop: 12 }}
        title={t('pages.protect.accessRecover')}
        description={<code>x-ui setting -resetPanelAccess && x-ui restart</code>}
      />
    </Card>
  );
}

function AccessForm({
  initial,
  callerIp,
  onChanged,
}: {
  initial: string;
  callerIp: string;
  onChanged: () => void;
}) {
  const uid = useId();
  const { t } = useTranslation();
  const [value, setValue] = useState(initial.split(',').filter(Boolean).join('\n'));
  const save = useMutation({
    mutationFn: () => HttpUtil.post(`${API}/access`, { allowIps: value }, JSON_HEADERS),
    onSuccess: (msg) => {
      if (msg?.success) onChanged();
    },
  });
  return (
    <Form layout="vertical">
      <Form.Item
        htmlFor={`${uid}-input-textarea-148`}
        label={t('pages.protect.accessList')}
        extra={t('pages.protect.accessYourIp', { ip: callerIp })}
      >
        <Input.TextArea
          id={`${uid}-input-textarea-148`}
          value={value}
          onChange={(e) => setValue(e.target.value)}
          autoSize={{ minRows: 3, maxRows: 8 }}
          placeholder={'203.0.113.10\n198.51.100.0/24'}
          style={{ fontFamily: 'monospace' }}
        />
      </Form.Item>
      <Space wrap>
        <Button
          type="primary"
          icon={<SaveOutlined aria-hidden />}
          loading={save.isPending}
          onClick={() => save.mutate()}
        >
          {t('save')}
        </Button>
        <Button onClick={() => setValue((v) => (v.trim() ? `${v.trim()}\n${callerIp}` : callerIp))}>
          {t('pages.protect.accessAddMine')}
        </Button>
      </Space>
    </Form>
  );
}

function CfRow({
  label,
  domain: initialDomain,
  port,
  panel,
  cfOnly,
}: {
  label: string;
  domain: string;
  port: number;
  panel: boolean;
  cfOnly?: CfOnlyStatus;
}) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const limited = !!cfOnly?.ports.includes(port);
  const setCfOnly = useMutation({
    mutationFn: (on: boolean) =>
      HttpUtil.post<CfOnlyStatus>(`${API}/cf/only`, { port, on, panel }, JSON_HEADERS),
    onSuccess: (msg) => {
      if (msg?.obj) queryClient.setQueryData([...keys.protect.root(), 'cfOnly'], msg.obj);
    },
  });
  const [domain, setDomain] = useState(initialDomain);
  const [status, setStatus] = useState<CfStatus | null>(null);
  const [busy, setBusy] = useState(false);

  async function run(path: string, body: object) {
    setBusy(true);
    try {
      const msg = await HttpUtil.post<CfStatus>(`${API}${path}`, body, {
        ...JSON_HEADERS,
        silentSuccess: path === '/cf/status',
      });
      if (msg?.success && msg.obj) setStatus(msg.obj);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div style={{ marginBottom: 16 }}>
      <Typography.Text strong>{label}</Typography.Text>
      <Space wrap style={{ display: 'flex', marginTop: 6 }}>
        <Input
          aria-label={label}
          style={{ width: 260 }}
          value={domain}
          placeholder="panel.example.com"
          onChange={(e) => {
            setDomain(e.target.value);
            setStatus(null);
          }}
        />
        <Tag>{t('pages.protect.port', { port })}</Tag>
        <Button
          icon={<ReloadOutlined aria-hidden />}
          loading={busy}
          disabled={!domain.trim()}
          onClick={() => run('/cf/status', { domain, port })}
        >
          {t('pages.protect.cfCheck')}
        </Button>
        {status?.found && (
          <Switch
            aria-label={t('pages.protect.cfHidden')}
            checked={status.proxied}
            loading={busy}
            checkedChildren={t('pages.protect.cfHidden')}
            unCheckedChildren={t('pages.protect.cfExposed')}
            onChange={(on) => run('/cf/proxied', { domain, port, on, panel })}
          />
        )}
        {(limited || status?.proxied) && (
          <Space size={6}>
            <Switch
              aria-label={t('pages.protect.cfOnly')}
              checked={limited}
              loading={setCfOnly.isPending}
              disabled={!limited && !cfOnly?.defaultDeny}
              onChange={(on) => setCfOnly.mutate(on)}
            />
            <span>{t('pages.protect.cfOnly')}</span>
          </Space>
        )}
      </Space>
      {limited && (
        <Typography.Text type="secondary" style={{ display: 'block', fontSize: 12, marginTop: 4 }}>
          {t('pages.protect.cfOnlyOn', { port })}
        </Typography.Text>
      )}
      {status && (
        <Descriptions size="small" column={{ xs: 1, sm: 2, md: 4 }} style={{ marginTop: 8 }}>
          <Descriptions.Item label={t('pages.protect.cfRecord')}>
            {status.found ? status.ip : t('pages.protect.cfNoRecord')}
          </Descriptions.Item>
          <Descriptions.Item label={t('pages.protect.cfPort')}>
            {status.portOk ? (
              <Tag color="green">OK</Tag>
            ) : (
              <Tag color="red">{t('pages.protect.cfPortBad')}</Tag>
            )}
          </Descriptions.Item>
          <Descriptions.Item label={t('pages.protect.cfSsl')}>
            {status.sslMode || t('pages.protect.cfSslUnknown')}
          </Descriptions.Item>
        </Descriptions>
      )}
    </div>
  );
}

function CloudflareCard() {
  const { t } = useTranslation();
  const query = useQuery({
    queryKey: [...keys.protect.root(), 'cf'],
    queryFn: () => getObj<CfInfo>('/cf'),
  });
  const cfOnlyQuery = useQuery({
    queryKey: [...keys.protect.root(), 'cfOnly'],
    queryFn: () => getObj<CfOnlyStatus>('/cf/only'),
  });
  const info = query.data;
  const cfOnly = cfOnlyQuery.data;
  const host = window.location.hostname;
  return (
    <Card size="small" title={t('pages.protect.cfTitle')}>
      <Typography.Paragraph type="secondary">{t('pages.protect.cfDesc')}</Typography.Paragraph>
      <Typography.Paragraph type="secondary">{t('pages.protect.cfOnlyDesc')}</Typography.Paragraph>
      {cfOnly && !cfOnly.defaultDeny && (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 12 }}
          title={cfOnly.active ? t('pages.protect.cfOnlyDefaultAllow') : t('pages.protect.cfOnlyNoUfw')}
        />
      )}
      {info && !info.cfConfigured && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          title={t('pages.protect.cfNoToken')}
        />
      )}
      {info?.cfConfigured && (
        <>
          <CfRow
            label={t('pages.protect.cfPanel')}
            panel
            domain={info.panelDomain || (isIP(host) ? '' : host)}
            port={info.panelPort}
            cfOnly={cfOnly}
          />
          {info.subEnable && (
            <CfRow
              label={t('pages.protect.cfSub')}
              panel={false}
              domain={info.subDomain}
              port={info.subPort}
              cfOnly={cfOnly}
            />
          )}
        </>
      )}
    </Card>
  );
}

function SpeedupDetails({ st }: { st: SpeedupStatus }) {
  const { t } = useTranslation();
  return (
    <Space size={4} wrap>
      <Tag color={st.bbrOn ? 'green' : 'default'}>
        {st.bbrOn
          ? t('pages.protect.bbrOn')
          : `${t('pages.protect.cc')}: ${st.congestionControl || '?'}`}
      </Tag>
      <Tag>qdisc: {st.qdisc || '?'}</Tag>
      {st.applied && <Tag color="blue">{t('pages.protect.tuned')}</Tag>}
      {st.kernel && <Tag bordered={false}>{st.kernel}</Tag>}
    </Space>
  );
}

function SpeedupCard() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: [...keys.protect.root(), 'speedup'],
    queryFn: () => getObj<SpeedupStatus>('/speedup'),
    retry: false,
  });
  const act = useMutation({
    mutationFn: (what: 'apply' | 'revert') =>
      HttpUtil.post<SpeedupStatus>(`${API}/speedup/${what}`),
    onSuccess: (msg) => {
      if (msg?.success) queryClient.setQueryData([...keys.protect.root(), 'speedup'], msg.obj);
    },
  });
  const st = query.data;
  return (
    <Card size="small" title={t('pages.protect.speedupTitle')}>
      <Typography.Paragraph type="secondary">{t('pages.protect.speedupDesc')}</Typography.Paragraph>
      {query.error && <Alert type="warning" showIcon title={String(query.error.message)} />}
      {st && (
        <Space orientation="vertical" size={12}>
          <SpeedupDetails st={st} />
          <Space wrap>
            <Button
              type="primary"
              icon={<ThunderboltOutlined aria-hidden />}
              loading={act.isPending && act.variables === 'apply'}
              onClick={() => act.mutate('apply')}
            >
              {st.applied ? t('pages.protect.speedupReapply') : t('pages.protect.speedupApply')}
            </Button>
            <Button
              disabled={!st.applied}
              loading={act.isPending && act.variables === 'revert'}
              onClick={() => act.mutate('revert')}
            >
              {t('pages.protect.speedupRevert')}
            </Button>
          </Space>
        </Space>
      )}
    </Card>
  );
}

function NodesCard() {
  const { t } = useTranslation();
  const [modal, modalContextHolder] = Modal.useModal();
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: keys.protect.nodes(),
    queryFn: () => getObj<NodeRow[]>('/nodes'),
  });
  const act = useMutation({
    mutationFn: ({ id, action }: { id: number; action: string }) =>
      HttpUtil.post(`${API}/nodes/${id}/${action}`),
    onSuccess: (msg) => {
      if (msg?.success) queryClient.invalidateQueries({ queryKey: keys.protect.nodes() });
    },
  });

  function confirmLock(row: NodeRow) {
    modal.confirm({
      title: t('pages.protect.lockConfirm', { name: row.name }),
      content: t('pages.protect.lockConfirmDesc'),
      okText: t('confirm'),
      cancelText: t('cancel'),
      onOk: () => act.mutateAsync({ id: row.id, action: 'lock' }),
    });
  }

  const columns: TableColumnsType<NodeRow> = [
    { title: t('pages.protect.node'), dataIndex: 'name', key: 'name' },
    {
      title: t('pages.protect.speedupTitle'),
      key: 'speedup',
      render: (_v, row) =>
        !row.online ? (
          <Tag>{t('pages.protect.offline')}</Tag>
        ) : row.speedup ? (
          <SpeedupDetails st={row.speedup} />
        ) : (
          <Typography.Text type="secondary">{row.error || '—'}</Typography.Text>
        ),
    },
    {
      title: t('pages.protect.accessTitle'),
      key: 'access',
      render: (_v, row) =>
        row.allowIps === undefined ? (
          '—'
        ) : row.allowIps ? (
          <Tag color="green" icon={<LockOutlined aria-hidden />}>
            {row.allowIps}
          </Tag>
        ) : (
          <Tag icon={<UnlockOutlined aria-hidden />}>{t('pages.protect.open')}</Tag>
        ),
    },
    {
      title: t('pages.clients.actions'),
      key: 'actions',
      render: (_v, row) => (
        <Space wrap size={4}>
          <Button
            size="small"
            disabled={!row.online}
            icon={<ThunderboltOutlined aria-hidden />}
            onClick={() => act.mutate({ id: row.id, action: 'speedupApply' })}
          >
            {t('pages.protect.speedupApply')}
          </Button>
          <Button
            size="small"
            disabled={!row.online || !row.speedup?.applied}
            onClick={() => act.mutate({ id: row.id, action: 'speedupRevert' })}
          >
            {t('pages.protect.speedupRevert')}
          </Button>
          {row.allowIps ? (
            <Button
              size="small"
              disabled={!row.online}
              icon={<UnlockOutlined aria-hidden />}
              onClick={() => act.mutate({ id: row.id, action: 'unlock' })}
            >
              {t('pages.protect.unlock')}
            </Button>
          ) : (
            <Button
              size="small"
              disabled={!row.online || row.allowIps === undefined}
              icon={<EyeInvisibleOutlined aria-hidden />}
              onClick={() => confirmLock(row)}
            >
              {t('pages.protect.lock')}
            </Button>
          )}
        </Space>
      ),
    },
  ];

  return (
    <Card
      size="small"
      title={t('pages.protect.nodesTitle')}
      extra={
        <Button size="small" icon={<ReloadOutlined aria-hidden />} onClick={() => query.refetch()}>
          {t('refresh')}
        </Button>
      }
    >
      {modalContextHolder}
      <Typography.Paragraph type="secondary">{t('pages.protect.nodesDesc')}</Typography.Paragraph>
      <Table<NodeRow>
        dataSource={query.data ?? []}
        columns={columns}
        rowKey="id"
        size="small"
        pagination={false}
        loading={query.isFetching || act.isPending}
        scroll={{ x: 'max-content' }}
      />
    </Card>
  );
}

export default function ProtectPage() {
  usePageTitle();
  const { isDark, isUltra, antdThemeConfig } = useTheme();
  const [messageApi, messageContextHolder] = message.useMessage();
  useEffect(() => {
    setMessageInstance(messageApi);
  }, [messageApi]);

  const pageClass = useMemo(() => {
    const classes = ['protect-page'];
    if (isDark) classes.push('is-dark');
    if (isUltra) classes.push('is-ultra');
    return classes.join(' ');
  }, [isDark, isUltra]);

  return (
    <ConfigProvider theme={antdThemeConfig}>
      {messageContextHolder}
      <Layout className={pageClass}>
        <AppSidebar />
        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            <Space orientation="vertical" size={16} style={{ width: '100%' }}>
              <DecoyCard />
              <RealitySelfCard />
              <AccessCard />
              <CloudflareCard />
              <SpeedupCard />
              <FirewallCard />
              <NodesCard />
            </Space>
          </Layout.Content>
        </Layout>
      </Layout>
    </ConfigProvider>
  );
}
