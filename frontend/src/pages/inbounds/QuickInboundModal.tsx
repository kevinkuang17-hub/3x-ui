import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Checkbox,
  Descriptions,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Spin,
  Tag,
  Typography,
  message,
} from 'antd';
import { ReloadOutlined } from '@ant-design/icons';

import { HttpUtil } from '@/utils';
import {
  QUICK_PRESETS,
  assignPorts,
  buildQuickInbound,
  findDuplicate,
  presetBlocked,
  presetWarning,
  recommendedPresets,
  uniqueRemark,
  type ExistingInbound,
  type QuickPresetId,
  type QuickServerFacts,
} from '@/lib/xray/inbound-presets';
import { formValuesToWirePayload } from '@/lib/xray/inbound-form-adapter';
import { decoyMasquerade } from '@/lib/xray/hysteria-masquerade';
import { allowFirewall, type FirewallRule } from '@/pages/protect/FirewallCard';
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
  inbounds: readonly ExistingInbound[];
  onClose: () => void;
  onCreated: () => void;
  onAdvanced: (values: InboundFormValues) => void;
}

interface RealityTarget {
  target: string;
  serverNames: string[];
}

interface Row {
  id: QuickPresetId;
  remark: string;
  port: number;
}

function formatDate(ms: number): string {
  return ms > 0 ? new Date(ms).toLocaleDateString() : '';
}

function factsOf(rec: InboundRecommendation): QuickServerFacts {
  const resolved = (rec.domainIps ?? []).length > 0;
  return {
    domain: rec.domain,
    certOk: rec.certState === 'ok' || rec.certState === 'unverified',
    pointsHere: rec.pointsHere,
    behindProxy: resolved && !rec.pointsHere,
  };
}

const usesReality = (id: QuickPresetId) => id === 'reality-vision' || id === 'xhttp-reality';

export default function QuickInboundModal({
  open,
  nodes,
  inbounds,
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
  const [baseName, setBaseName] = useState('');
  const [checked, setChecked] = useState<QuickPresetId[]>([]);
  const [remarkEdits, setRemarkEdits] = useState<Partial<Record<QuickPresetId, string>>>({});
  const [portEdits, setPortEdits] = useState<Partial<Record<QuickPresetId, number>>>({});
  const [reality, setReality] = useState<RealityTarget | null>(null);
  const [scanning, setScanning] = useState(false);
  const [scanned, setScanned] = useState(false);
  const [clientsChoice, setClientsChoice] = useState<string>(ALL_CLIENTS);
  const [hyDecoy, setHyDecoy] = useState(true);
  const [openFirewall, setOpenFirewall] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const { data: clients = [] } = useClientRecords(open);

  const onlineNodes = useMemo(
    () => (nodes || []).filter((n) => n.enable && n.status === 'online'),
    [nodes],
  );
  const nodeId = server === LOCAL ? null : server;
  const firewallOn = server === LOCAL && rec?.firewall === 'ufw';
  const facts = rec ? factsOf(rec) : null;

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
        const next = msg.obj;
        const f = factsOf(next);
        setRec(next);
        setRemarkEdits({});
        setPortEdits({});
        setBaseName(onlineNodes.find((n) => n.id === server)?.name ?? '');
        setChecked(
          recommendedPresets(f).filter((id) => {
            const preset = QUICK_PRESETS.find((p) => p.id === id)!;
            return !presetBlocked(preset, f) && !findDuplicate(preset, nodeId, inbounds);
          }),
        );
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
      const usable = (Array.isArray(msg?.obj) ? msg.obj : []).filter(
        (r) => r.feasible && !r.privateTarget && (r.serverNames?.length ?? 0) > 0,
      );
      // XHTTP over REALITY needs a target that speaks HTTP/2.
      const best = usable.find((r) => r.h2) ?? usable[0];
      setReality(best ? { target: best.target, serverNames: best.serverNames ?? [] } : null);
    } finally {
      setScanned(true);
      setScanning(false);
    }
  };

  const needsReality = checked.some(usesReality);
  useEffect(() => {
    if (open && needsReality && !scanned && !scanning) void scanReality();
    /* eslint-disable-next-line react-hooks/exhaustive-deps */
  }, [open, needsReality]);

  useEffect(() => {
    if (open) return;
    setServer(LOCAL);
    setReality(null);
    setScanned(false);
    setClientsChoice(ALL_CLIENTS);
  }, [open]);

  const autoPorts = useMemo(
    () => assignPorts(checked, { tcp: rec?.tcpPorts ?? [], udp: rec?.udpPorts ?? [] }),
    [checked, rec],
  );

  const rows: Row[] = useMemo(() => {
    const taken = new Set(inbounds.map((ib) => ib.remark.trim()).filter(Boolean));
    const out: Row[] = [];
    for (const preset of QUICK_PRESETS) {
      if (!checked.includes(preset.id)) continue;
      const remark = remarkEdits[preset.id] ?? uniqueRemark(baseName, preset.label, taken);
      taken.add(remark.trim());
      out.push({
        id: preset.id,
        remark,
        port: portEdits[preset.id] ?? autoPorts.get(preset.id) ?? 0,
      });
    }
    return out;
  }, [checked, remarkEdits, portEdits, autoPorts, baseName, inbounds]);

  const problem = useMemo((): string => {
    if (rows.length === 0) return t('pages.inbounds.quick.selectOne');
    const existing = new Set(inbounds.map((ib) => ib.remark.trim()).filter(Boolean));
    const seen = new Set<string>();
    for (const row of rows) {
      const remark = row.remark.trim();
      if (!remark || existing.has(remark) || seen.has(remark))
        return t('pages.inbounds.quick.remarkTaken', { remark: remark || '—' });
      seen.add(remark);
      if (!row.port) return t('pages.inbounds.quick.noPort');
    }
    const preset = (id: QuickPresetId) => QUICK_PRESETS.find((p) => p.id === id)!;
    for (const a of rows) {
      for (const b of rows) {
        if (a.id >= b.id || a.port !== b.port) continue;
        const ta = preset(a.id).transport;
        const tb = preset(b.id).transport;
        if (ta === tb || ta === 'both' || tb === 'both')
          return t('pages.inbounds.quick.portTaken', { port: a.port });
      }
    }
    if (rows.some((r) => usesReality(r.id)) && !reality)
      return t('pages.inbounds.quick.targetNone');
    return '';
  }, [rows, inbounds, reality, t]);

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

  const buildValues = async (row: Row): Promise<InboundFormValues | null> => {
    if (!rec) return null;
    let masquerade;
    if (row.id === 'hysteria2' && hyDecoy) {
      try {
        masquerade = await decoyMasquerade();
      } catch (err) {
        messageApi.warning(
          t('pages.inbounds.quick.hyDecoyMissing', {
            msg: err instanceof Error ? err.message : String(err),
          }),
        );
      }
    }
    let keys = { privateKey: '', publicKey: '' };
    if (usesReality(row.id)) {
      const msg = await HttpUtil.get<{ privateKey: string; publicKey: string }>(
        '/panel/api/server/getNewX25519Cert',
      );
      if (!msg?.success || !msg.obj) return null;
      keys = msg.obj;
    }
    return buildQuickInbound({
      preset: row.id,
      remark: row.remark.trim(),
      port: row.port,
      nodeId,
      domain: rec.domain,
      certFile: rec.certFile,
      keyFile: rec.keyFile,
      realityTarget: reality?.target ?? '',
      realityServerNames: reality?.serverNames ?? [],
      realityPrivateKey: keys.privateKey,
      realityPublicKey: keys.publicKey,
      masquerade,
    });
  };

  const create = async () => {
    setSubmitting(true);
    try {
      const createdIds: number[] = [];
      const opened: FirewallRule[] = [];
      const failed: string[] = [];
      for (const row of rows) {
        const values = await buildValues(row);
        const parsed = values ? InboundFormSchema.safeParse(values) : null;
        if (!parsed?.success) {
          failed.push(row.remark);
          continue;
        }
        const msg = await HttpUtil.post(
          '/panel/api/inbounds/add',
          formValuesToWirePayload(parsed.data),
          {
            silentSuccess: true,
          },
        );
        const id = (msg?.obj as { id?: number } | null)?.id;
        if (msg?.success && id) {
          createdIds.push(id);
          const transport = QUICK_PRESETS.find((p) => p.id === row.id)!.transport;
          if (transport !== 'udp') opened.push({ port: row.port, proto: 'tcp' });
          if (transport !== 'tcp') opened.push({ port: row.port, proto: 'udp' });
        } else failed.push(row.remark);
      }
      if (opened.length > 0 && firewallOn && openFirewall) await allowFirewall(opened);
      const emails = selectedEmails();
      if (createdIds.length > 0 && emails.length > 0) {
        await HttpUtil.post(
          '/panel/api/clients/bulkAttach',
          { emails, inboundIds: createdIds },
          { ...JSON_HEADERS, silent: true },
        );
      }
      if (createdIds.length > 0) {
        messageApi.success(
          t('pages.inbounds.quick.createdSummary', {
            count: createdIds.length,
            clients: emails.length,
          }),
        );
        onCreated();
      }
      if (failed.length > 0) {
        messageApi.error(t('pages.inbounds.quick.createFailed', { list: failed.join(', ') }));
        return;
      }
      onClose();
    } finally {
      setSubmitting(false);
    }
  };

  const advanced = async () => {
    if (rows.length !== 1) return;
    setSubmitting(true);
    try {
      const values = await buildValues(rows[0]);
      if (!values) return;
      onClose();
      onAdvanced(values);
    } finally {
      setSubmitting(false);
    }
  };

  const toggle = (id: QuickPresetId, on: boolean) =>
    setChecked((prev) => (on ? [...prev, id] : prev.filter((x) => x !== id)));

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

  const recommended = facts ? recommendedPresets(facts) : [];

  return (
    <Modal
      open={open}
      width={720}
      title={t('pages.inbounds.quick.title')}
      onCancel={onClose}
      destroyOnHidden
      footer={[
        <Button key="cancel" onClick={onClose}>
          {t('cancel')}
        </Button>,
        <Button
          key="advanced"
          disabled={!!problem || rows.length !== 1 || submitting}
          onClick={advanced}
        >
          {t('pages.inbounds.quick.advanced')}
        </Button>,
        <Button
          key="create"
          type="primary"
          loading={submitting}
          disabled={!rec || !!problem}
          onClick={create}
        >
          {t('pages.inbounds.quick.create', { count: rows.length })}
        </Button>,
      ]}
    >
      {messageContextHolder}
      <Space direction="vertical" size={14} className="quick-inbound">
        <Space wrap size={12} className="quick-inbound-row">
          {onlineNodes.length > 0 && (
            <Space direction="vertical" size={4}>
              <Typography.Text>{t('pages.inbounds.quick.server')}</Typography.Text>
              <Select
                value={server}
                onChange={setServer}
                className="quick-inbound-server"
                options={[
                  { value: LOCAL, label: t('pages.inbounds.localPanel') },
                  ...onlineNodes.map((n) => ({ value: n.id, label: n.name })),
                ]}
              />
            </Space>
          )}
          <Space direction="vertical" size={4}>
            <Typography.Text>{t('pages.inbounds.quick.baseName')}</Typography.Text>
            <Input
              value={baseName}
              placeholder={t('pages.inbounds.quick.remarkPlaceholder')}
              onChange={(e) => {
                setBaseName(e.target.value);
                setRemarkEdits({});
              }}
            />
          </Space>
        </Space>

        {loading && (
          <Space>
            <Spin size="small" />
            <Typography.Text type="secondary">
              {t('pages.inbounds.quick.detecting')}
            </Typography.Text>
          </Space>
        )}
        {recError && <Alert type="error" showIcon message={recError} />}

        {rec && facts && !loading && (
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

            <div className="quick-inbound-presets">
              {QUICK_PRESETS.map((preset) => {
                const dup = findDuplicate(preset, nodeId, inbounds);
                const blocked = presetBlocked(preset, facts);
                const warning = presetWarning(preset, facts);
                const row = rows.find((r) => r.id === preset.id);
                return (
                  <div
                    key={preset.id}
                    className={`quick-inbound-preset${row ? ' is-checked' : ''}`}
                    data-preset={preset.id}
                  >
                    <Checkbox
                      checked={!!row}
                      disabled={!!dup || blocked}
                      onChange={(e) => toggle(preset.id, e.target.checked)}
                    >
                      <Space size={6} wrap>
                        <Typography.Text strong>
                          {t(`pages.inbounds.quick.presets.${preset.id}.title`)}
                        </Typography.Text>
                        {recommended.includes(preset.id) && !dup && !blocked && (
                          <Tag color="green">{t('pages.inbounds.quick.recommended')}</Tag>
                        )}
                        {dup && (
                          <Tag>
                            {t('pages.inbounds.quick.exists', {
                              remark: dup.remark,
                              port: dup.port,
                            })}
                          </Tag>
                        )}
                        {!dup && blocked && (
                          <Tag color="warning">{t('pages.inbounds.quick.needsCert')}</Tag>
                        )}
                      </Space>
                    </Checkbox>
                    <Typography.Paragraph type="secondary" className="quick-inbound-desc">
                      {t(`pages.inbounds.quick.presets.${preset.id}.desc`)}
                    </Typography.Paragraph>
                    {row && preset.id === 'hysteria2' && (
                      <div className="quick-inbound-desc">
                        <Checkbox checked={hyDecoy} onChange={(e) => setHyDecoy(e.target.checked)}>
                          {t('pages.inbounds.quick.hyDecoy')}
                        </Checkbox>
                        <Typography.Text type="secondary" className="quick-inbound-desc">
                          {t('pages.inbounds.quick.hyUdpNote', { port: row.port })}
                        </Typography.Text>
                      </div>
                    )}
                    {row && warning && (
                      <Typography.Text type="warning" className="quick-inbound-desc">
                        {t(`pages.inbounds.quick.${warning}`)}
                      </Typography.Text>
                    )}
                    {row && (
                      <Space wrap size={8} className="quick-inbound-fields">
                        <Input
                          size="small"
                          prefix={t('pages.inbounds.quick.remark')}
                          value={row.remark}
                          onChange={(e) =>
                            setRemarkEdits((prev) => ({ ...prev, [preset.id]: e.target.value }))
                          }
                        />
                        <InputNumber
                          size="small"
                          prefix={t('pages.inbounds.quick.port')}
                          min={1}
                          max={65535}
                          value={row.port || null}
                          onChange={(v) =>
                            setPortEdits((prev) => ({ ...prev, [preset.id]: v ?? 0 }))
                          }
                        />
                        <Tag>
                          {preset.transport === 'both' ? 'TCP+UDP' : preset.transport.toUpperCase()}
                        </Tag>
                      </Space>
                    )}
                  </div>
                );
              })}
            </div>

            {firewallOn && (
              <Checkbox checked={openFirewall} onChange={(e) => setOpenFirewall(e.target.checked)}>
                {t('pages.inbounds.quick.openFirewall')}
              </Checkbox>
            )}

            {needsReality && (
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

            {problem && rows.length > 0 && <Alert type="warning" showIcon message={problem} />}
          </>
        )}
      </Space>
    </Modal>
  );
}
