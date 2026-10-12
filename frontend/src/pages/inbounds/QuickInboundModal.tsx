import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Descriptions,
  Input,
  InputNumber,
  Modal,
  Radio,
  Select,
  Space,
  Spin,
  Tag,
  Typography,
  message,
} from 'antd';
import { ReloadOutlined } from '@ant-design/icons';

import { HttpUtil } from '@/utils';
import { buildQuickInbound, type QuickInboundKind } from '@/lib/xray/inbound-presets';
import { formValuesToWirePayload } from '@/lib/xray/inbound-form-adapter';
import { InboundFormSchema, type InboundFormValues } from '@/schemas/forms/inbound-form';
import type { InboundRecommendation, RealityScanResult } from '@/generated/types';
import type { NodeRecord } from '@/api/queries/useNodesQuery';
import { useClientRecords } from '@/api/queries/useClientOptions';

import './QuickInboundModal.css';

const LOCAL = 0;
const NO_CLIENTS = 'none';
const ALL_CLIENTS = 'all';
const GROUP_PREFIX = 'group:';
const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;

interface QuickInboundModalProps {
  open: boolean;
  nodes: NodeRecord[];
  onClose: () => void;
  onCreated: () => void;
  onAdvanced: (values: InboundFormValues) => void;
}

interface RealityTarget {
  target: string;
  serverNames: string[];
}

function tlsBlocker(rec: InboundRecommendation | null): string {
  if (!rec?.domain) return 'pages.inbounds.quick.domainNone';
  switch (rec.certState) {
    case 'ok':
    case 'unverified':
      return '';
    case 'expired':
      return 'pages.inbounds.quick.certExpired';
    case 'mismatch':
      return 'pages.inbounds.quick.certMismatch';
    case 'unreadable':
      return 'pages.inbounds.quick.certUnreadable';
    default:
      return 'pages.inbounds.quick.certMissing';
  }
}

function formatDate(ms: number): string {
  return ms > 0 ? new Date(ms).toLocaleDateString() : '';
}

export default function QuickInboundModal({
  open,
  nodes,
  onClose,
  onCreated,
  onAdvanced,
}: QuickInboundModalProps) {
  const { t } = useTranslation();
  const [messageApi, messageContextHolder] = message.useMessage();
  const [server, setServer] = useState<number>(LOCAL);
  const [rec, setRec] = useState<InboundRecommendation | null>(null);
  const [recError, setRecError] = useState('');
  const [loading, setLoading] = useState(false);
  const [kind, setKind] = useState<QuickInboundKind>('reality');
  const [remark, setRemark] = useState('');
  const [port, setPort] = useState<number | null>(null);
  const [reality, setReality] = useState<RealityTarget | null>(null);
  const [scanning, setScanning] = useState(false);
  const [scanned, setScanned] = useState(false);
  const [clientsChoice, setClientsChoice] = useState<string>(ALL_CLIENTS);
  const [submitting, setSubmitting] = useState(false);
  const { data: clients = [] } = useClientRecords(open);

  const onlineNodes = useMemo(
    () => (nodes || []).filter((n) => n.enable && n.status === 'online'),
    [nodes],
  );
  const nodeName = (id: number) => onlineNodes.find((n) => n.id === id)?.name ?? '';

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    setLoading(true);
    setRec(null);
    setRecError('');
    const query = server === LOCAL ? '' : `?nodeId=${server}`;
    HttpUtil.get<InboundRecommendation>(`/panel/api/inbounds/recommend${query}`, undefined, {
      silent: true,
    })
      .then((msg) => {
        if (cancelled) return;
        if (!msg?.success || !msg.obj) {
          setRecError(msg?.msg || t('somethingWentWrong'));
          return;
        }
        setRec(msg.obj);
        setKind(msg.obj.recommended === 'tls' ? 'tls' : 'reality');
        setPort(msg.obj.port || null);
        setRemark(server === LOCAL ? '' : nodeName(server));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
    /* eslint-disable-next-line react-hooks/exhaustive-deps */
  }, [open, server]);

  const scanReality = async () => {
    setScanning(true);
    try {
      const msg = await HttpUtil.post<RealityScanResult[]>(
        '/panel/api/server/scanRealityTargets',
        {},
        { silent: true },
      );
      const best = (Array.isArray(msg?.obj) ? msg.obj : []).find(
        (r) => r.feasible && !r.privateTarget && (r.serverNames?.length ?? 0) > 0,
      );
      setReality(best ? { target: best.target, serverNames: best.serverNames ?? [] } : null);
    } finally {
      setScanned(true);
      setScanning(false);
    }
  };

  useEffect(() => {
    if (open && kind === 'reality' && !scanned && !scanning) void scanReality();
    /* eslint-disable-next-line react-hooks/exhaustive-deps */
  }, [open, kind]);

  useEffect(() => {
    if (open) return;
    setServer(LOCAL);
    setReality(null);
    setScanned(false);
    setClientsChoice(ALL_CLIENTS);
  }, [open]);

  const groups = useMemo(() => {
    const counts = new Map<string, number>();
    for (const c of clients) {
      const g = (c.group ?? '').trim();
      if (g) counts.set(g, (counts.get(g) ?? 0) + 1);
    }
    return [...counts.entries()].sort((a, b) => a[0].localeCompare(b[0]));
  }, [clients]);

  const selectedEmails = (): string[] => {
    if (clientsChoice === NO_CLIENTS) return [];
    if (clientsChoice === ALL_CLIENTS) return clients.map((c) => c.email);
    const group = clientsChoice.slice(GROUP_PREFIX.length);
    return clients.filter((c) => (c.group ?? '').trim() === group).map((c) => c.email);
  };

  const blocker = tlsBlocker(rec);
  const realityMissing = kind === 'reality' && !reality;
  const canSubmit = !!rec && !!port && !(kind === 'tls' && blocker) && !realityMissing;

  const buildValues = async (): Promise<InboundFormValues | null> => {
    if (!rec || !port) return null;
    let keys = { privateKey: '', publicKey: '' };
    if (kind === 'reality') {
      const msg = await HttpUtil.get<{ privateKey: string; publicKey: string }>(
        '/panel/api/server/getNewX25519Cert',
      );
      if (!msg?.success || !msg.obj) return null;
      keys = msg.obj;
    }
    return buildQuickInbound({
      kind,
      remark: remark.trim(),
      port,
      nodeId: server === LOCAL ? null : server,
      domain: rec.domain,
      certFile: rec.certFile,
      keyFile: rec.keyFile,
      realityTarget: reality?.target ?? '',
      realityServerNames: reality?.serverNames ?? [],
      realityPrivateKey: keys.privateKey,
      realityPublicKey: keys.publicKey,
    });
  };

  const create = async () => {
    setSubmitting(true);
    try {
      const values = await buildValues();
      if (!values) return;
      const parsed = InboundFormSchema.safeParse(values);
      if (!parsed.success) {
        messageApi.error(parsed.error.issues[0]?.message || t('somethingWentWrong'));
        return;
      }
      const msg = await HttpUtil.post(
        '/panel/api/inbounds/add',
        formValuesToWirePayload(parsed.data),
      );
      if (!msg?.success) return;
      const created = msg.obj as { id?: number } | null;
      const emails = selectedEmails();
      if (created?.id && emails.length > 0) {
        await HttpUtil.post(
          '/panel/api/clients/bulkAttach',
          { emails, inboundIds: [created.id] },
          { ...JSON_HEADERS, silent: true },
        );
        messageApi.success(t('pages.inbounds.quick.createdWithClients', { count: emails.length }));
      }
      onCreated();
      onClose();
    } finally {
      setSubmitting(false);
    }
  };

  const advanced = async () => {
    setSubmitting(true);
    try {
      const values = await buildValues();
      if (!values) return;
      onClose();
      onAdvanced(values);
    } finally {
      setSubmitting(false);
    }
  };

  const ips = (list: string[] | null | undefined) => (list ?? []).join(', ');
  const dnsLine = () => {
    if (!rec?.domain) return <Typography.Text type="secondary">—</Typography.Text>;
    if (rec.pointsHere)
      return (
        <Tag color="success">
          {t('pages.inbounds.quick.dnsHere', { ips: ips(rec.domainIps) || rec.domain })}
        </Tag>
      );
    if ((rec.domainIps ?? []).length === 0)
      return <Tag color="warning">{t('pages.inbounds.quick.dnsNone')}</Tag>;
    return (
      <Tag color="warning">
        {t('pages.inbounds.quick.dnsElsewhere', { ips: ips(rec.domainIps) })}
      </Tag>
    );
  };
  const certLine = () => {
    if (!rec) return null;
    const date = formatDate(rec.certNotAfter);
    switch (rec.certState) {
      case 'ok':
        return <Tag color="success">{t('pages.inbounds.quick.certOk', { date })}</Tag>;
      case 'unverified':
        return <Tag color="processing">{t('pages.inbounds.quick.certUnverified')}</Tag>;
      case 'expired':
        return <Tag color="error">{t('pages.inbounds.quick.certExpired', { date })}</Tag>;
      case 'mismatch':
        return (
          <Tag color="error">{t('pages.inbounds.quick.certMismatch', { domain: rec.domain })}</Tag>
        );
      case 'unreadable':
        return <Tag color="error">{t('pages.inbounds.quick.certUnreadable')}</Tag>;
      default:
        return <Tag>{t('pages.inbounds.quick.certMissing')}</Tag>;
    }
  };

  const kindText = {
    tls: { title: t('pages.inbounds.quick.tlsTitle'), desc: t('pages.inbounds.quick.tlsDesc') },
    reality: {
      title: t('pages.inbounds.quick.realityTitle'),
      desc: t('pages.inbounds.quick.realityDesc'),
    },
  };
  const kindCard = (value: QuickInboundKind) => {
    const disabled = value === 'tls' && !!blocker;
    return (
      <Radio value={value} disabled={disabled} className="quick-inbound-kind">
        <Space direction="vertical" size={2}>
          <Space size={6} wrap>
            <Typography.Text strong>{kindText[value].title}</Typography.Text>
            {rec?.recommended === value && (
              <Tag color="green">{t('pages.inbounds.quick.recommended')}</Tag>
            )}
          </Space>
          <Typography.Text type="secondary">{kindText[value].desc}</Typography.Text>
          {disabled && (
            <Typography.Text type="warning">
              {t('pages.inbounds.quick.tlsUnavailable')}
            </Typography.Text>
          )}
        </Space>
      </Radio>
    );
  };

  return (
    <Modal
      open={open}
      title={t('pages.inbounds.quick.title')}
      onCancel={onClose}
      destroyOnHidden
      footer={[
        <Button key="cancel" onClick={onClose}>
          {t('cancel')}
        </Button>,
        <Button key="advanced" disabled={!canSubmit || submitting} onClick={advanced}>
          {t('pages.inbounds.quick.advanced')}
        </Button>,
        <Button
          key="create"
          type="primary"
          loading={submitting}
          disabled={!canSubmit}
          onClick={create}
        >
          {t('pages.inbounds.quick.create')}
        </Button>,
      ]}
    >
      {messageContextHolder}
      <Space direction="vertical" size={14} className="quick-inbound">
        {onlineNodes.length > 0 && (
          <Space direction="vertical" size={4} className="quick-inbound-row">
            <Typography.Text>{t('pages.inbounds.quick.server')}</Typography.Text>
            <Select
              value={server}
              onChange={setServer}
              options={[
                { value: LOCAL, label: t('pages.inbounds.localPanel') },
                ...onlineNodes.map((n) => ({ value: n.id, label: n.name })),
              ]}
            />
          </Space>
        )}

        {loading && (
          <Space>
            <Spin size="small" />
            <Typography.Text type="secondary">
              {t('pages.inbounds.quick.detecting')}
            </Typography.Text>
          </Space>
        )}
        {recError && <Alert type="error" showIcon message={recError} />}

        {rec && !loading && (
          <>
            <Descriptions size="small" column={1} bordered>
              <Descriptions.Item label={t('pages.inbounds.quick.domain')}>
                {rec.domain || (
                  <Typography.Text type="secondary">
                    {t('pages.inbounds.quick.domainNone')}
                  </Typography.Text>
                )}
              </Descriptions.Item>
              <Descriptions.Item label={t('pages.inbounds.quick.dns')}>
                {dnsLine()}
              </Descriptions.Item>
              <Descriptions.Item label={t('pages.inbounds.quick.cert')}>
                {certLine()}
              </Descriptions.Item>
            </Descriptions>

            <Radio.Group
              value={kind}
              onChange={(e) => setKind(e.target.value as QuickInboundKind)}
              className="quick-inbound-kinds"
            >
              {kindCard('tls')}
              {kindCard('reality')}
            </Radio.Group>

            {kind === 'reality' && (
              <Space size={8} wrap>
                <Typography.Text>{t('pages.inbounds.quick.target')}:</Typography.Text>
                {scanning ? (
                  <Typography.Text type="secondary">
                    {t('pages.inbounds.quick.targetScanning')}
                  </Typography.Text>
                ) : reality ? (
                  <Typography.Text code>{reality.serverNames[0] || reality.target}</Typography.Text>
                ) : (
                  <Typography.Text type="warning">
                    {t('pages.inbounds.quick.targetNone')}
                  </Typography.Text>
                )}
                <Button
                  size="small"
                  icon={<ReloadOutlined />}
                  loading={scanning}
                  onClick={() => void scanReality()}
                >
                  {t('pages.inbounds.quick.rescan')}
                </Button>
              </Space>
            )}

            <Space wrap size={12} className="quick-inbound-row">
              <Space direction="vertical" size={4}>
                <Typography.Text>{t('pages.inbounds.quick.remark')}</Typography.Text>
                <Input
                  value={remark}
                  placeholder={t('pages.inbounds.quick.remarkPlaceholder')}
                  onChange={(e) => setRemark(e.target.value)}
                />
              </Space>
              <Space direction="vertical" size={4}>
                <Typography.Text>{t('pages.inbounds.quick.port')}</Typography.Text>
                <InputNumber min={1} max={65535} value={port} onChange={(v) => setPort(v)} />
              </Space>
            </Space>
            {rec.preferredPortBusy && rec.port > 0 && (
              <Typography.Text type="secondary">
                {t('pages.inbounds.quick.portBusy', { port: rec.port })}
              </Typography.Text>
            )}

            <Space direction="vertical" size={4} className="quick-inbound-row">
              <Typography.Text>{t('pages.inbounds.quick.clients')}</Typography.Text>
              <Select
                value={clientsChoice}
                onChange={setClientsChoice}
                options={[
                  {
                    value: ALL_CLIENTS,
                    label: t('pages.inbounds.quick.clientsAll', { count: clients.length }),
                  },
                  ...groups.map(([name, count]) => ({
                    value: `${GROUP_PREFIX}${name}`,
                    label: t('pages.inbounds.quick.clientsGroup', { name, count }),
                  })),
                  { value: NO_CLIENTS, label: t('pages.inbounds.quick.clientsNone') },
                ]}
              />
            </Space>
          </>
        )}
      </Space>
    </Modal>
  );
}
