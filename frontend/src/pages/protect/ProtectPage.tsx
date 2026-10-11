import { useEffect, useMemo, useState } from 'react';
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
  Select,
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
  UploadOutlined,
} from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { useTheme } from '@/hooks/useTheme';
import { usePageTitle } from '@/hooks/usePageTitle';
import { HttpUtil } from '@/utils';
import { setMessageInstance } from '@/utils/messageBus';
import AppSidebar from '@/layouts/AppSidebar';
import { keys } from '@/api/queryKeys';

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

interface DecoySettings {
  mode: string;
  title: string;
  modes: string[];
  customSite: boolean;
  secretPath: boolean;
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

function DecoyCard() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: [...keys.protect.root(), 'decoy'],
    queryFn: () => getObj<DecoySettings>('/decoy'),
  });
  return (
    <Card size="small" title={t('pages.protect.decoyTitle')}>
      {query.data && (
        <DecoyForm
          key={query.dataUpdatedAt}
          initial={query.data}
          onChanged={() => queryClient.invalidateQueries({ queryKey: keys.protect.root() })}
        />
      )}
    </Card>
  );
}

function DecoyForm({ initial, onChanged }: { initial: DecoySettings; onChanged: () => void }) {
  const { t } = useTranslation();
  const [mode, setMode] = useState(initial.mode);
  const [title, setTitle] = useState(initial.title);
  const [uploading, setUploading] = useState(false);
  const save = useMutation({
    mutationFn: () => HttpUtil.post(`${API}/decoy`, { mode, title }, JSON_HEADERS),
    onSuccess: (msg) => {
      if (msg?.success) onChanged();
    },
  });

  function upload() {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.zip';
    input.addEventListener('change', async (e) => {
      const file = (e.target as HTMLInputElement).files?.[0];
      if (!file) return;
      const form = new FormData();
      form.append('file', file);
      setUploading(true);
      try {
        const msg = await HttpUtil.post(`${API}/decoy/upload`, form, {
          headers: { 'Content-Type': 'multipart/form-data' },
        });
        if (msg?.success) onChanged();
      } finally {
        setUploading(false);
      }
    });
    input.click();
  }

  const titled = mode === 'company' || mode === 'blog' || mode === 'maintenance';
  return (
    <Form layout="vertical">
      <Typography.Paragraph type="secondary">{t('pages.protect.decoyDesc')}</Typography.Paragraph>
      {!initial.secretPath && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          title={t('pages.protect.decoyNeedsPath')}
        />
      )}
      <Space wrap align="end">
        <Form.Item label={t('pages.protect.decoyMode')}>
          <Select
            style={{ width: 220 }}
            value={mode}
            onChange={setMode}
            options={initial.modes.map((m) => ({
              value: m,
              label: t(`pages.protect.decoyMode_${m}`),
              disabled: m === 'custom' && !initial.customSite,
            }))}
          />
        </Form.Item>
        {titled && (
          <Form.Item label={t('pages.protect.decoySiteTitle')}>
            <Input
              style={{ width: 240 }}
              value={title}
              maxLength={80}
              placeholder={t('pages.protect.decoySiteTitlePlaceholder')}
              onChange={(e) => setTitle(e.target.value)}
            />
          </Form.Item>
        )}
        <Form.Item>
          <Space wrap>
            <Button
              type="primary"
              icon={<SaveOutlined />}
              loading={save.isPending}
              onClick={() => save.mutate()}
            >
              {t('save')}
            </Button>
            <Button icon={<UploadOutlined />} loading={uploading} onClick={upload}>
              {t('pages.protect.decoyUpload')}
            </Button>
            <Button href="/" target="_blank" rel="noreferrer">
              {t('pages.protect.decoyPreview')}
            </Button>
          </Space>
        </Form.Item>
      </Space>
      <Typography.Text type="secondary">
        {initial.customSite
          ? t('pages.protect.decoyCustomReady')
          : t('pages.protect.decoyUploadDesc')}
      </Typography.Text>
    </Form>
  );
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
        label={t('pages.protect.accessList')}
        extra={t('pages.protect.accessYourIp', { ip: callerIp })}
      >
        <Input.TextArea
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
          icon={<SaveOutlined />}
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
}: {
  label: string;
  domain: string;
  port: number;
}) {
  const { t } = useTranslation();
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
          icon={<ReloadOutlined />}
          loading={busy}
          disabled={!domain.trim()}
          onClick={() => run('/cf/status', { domain, port })}
        >
          {t('pages.protect.cfCheck')}
        </Button>
        {status?.found && (
          <Switch
            checked={status.proxied}
            loading={busy}
            checkedChildren={t('pages.protect.cfHidden')}
            unCheckedChildren={t('pages.protect.cfExposed')}
            onChange={(on) => run('/cf/proxied', { domain, port, on })}
          />
        )}
      </Space>
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
  const info = query.data;
  const host = window.location.hostname;
  return (
    <Card size="small" title={t('pages.protect.cfTitle')}>
      <Typography.Paragraph type="secondary">{t('pages.protect.cfDesc')}</Typography.Paragraph>
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
            domain={info.panelDomain || (isIP(host) ? '' : host)}
            port={info.panelPort}
          />
          {info.subEnable && (
            <CfRow label={t('pages.protect.cfSub')} domain={info.subDomain} port={info.subPort} />
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
              icon={<ThunderboltOutlined />}
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
          <Tag color="green" icon={<LockOutlined />}>
            {row.allowIps}
          </Tag>
        ) : (
          <Tag icon={<UnlockOutlined />}>{t('pages.protect.open')}</Tag>
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
            icon={<ThunderboltOutlined />}
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
              icon={<UnlockOutlined />}
              onClick={() => act.mutate({ id: row.id, action: 'unlock' })}
            >
              {t('pages.protect.unlock')}
            </Button>
          ) : (
            <Button
              size="small"
              disabled={!row.online || row.allowIps === undefined}
              icon={<EyeInvisibleOutlined />}
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
        <Button size="small" icon={<ReloadOutlined />} onClick={() => query.refetch()}>
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
              <AccessCard />
              <CloudflareCard />
              <SpeedupCard />
              <NodesCard />
            </Space>
          </Layout.Content>
        </Layout>
      </Layout>
    </ConfigProvider>
  );
}
