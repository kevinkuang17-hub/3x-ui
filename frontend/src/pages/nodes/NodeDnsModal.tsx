import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Checkbox, Form, Input, Modal, Space, Spin, Typography } from 'antd';

import { useQuery } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';
import type { NodeRecord } from '@/api/queries/useNodesQuery';

interface DnsRecord {
  nodeId: number;
  reportPath: string;
  name: string;
  lastIp: string;
  observedIp: string;
  syncedAt: number;
  lastError: string;
}

interface DnsList {
  configured: boolean;
  records: DnsRecord[];
  script: string;
}

interface Props {
  node: NodeRecord | null;
  onClose: () => void;
  onChanged: () => void;
}

const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;

function suggestLabel(name: string): string {
  return name
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
}

export default function NodeDnsModal({ node, onClose, onChanged }: Props) {
  const uid = useId();
  const { t } = useTranslation();
  const [busy, setBusy] = useState(false);
  const [nameEdit, setNameEdit] = useState<string | null>(null);
  const [ip, setIp] = useState('');
  const [useAsAddress, setUseAsAddress] = useState(true);
  const { data, isFetching: loading } = useQuery({
    queryKey: ['nodes', 'dns'],
    queryFn: async () => {
      const msg = await HttpUtil.get<DnsList>('/panel/api/nodes/dns/list', undefined, {
        silent: true,
      });
      return msg?.success && msg.obj ? msg.obj : { configured: false, records: [], script: '' };
    },
    enabled: node !== null,
    gcTime: 0,
  });
  const configured = data?.configured ?? true;
  const record = data?.records.find((r) => r.nodeId === node?.id) ?? null;
  const name = nameEdit ?? record?.name ?? suggestLabel(node?.name ?? '');
  const reporterCommand =
    record?.reportPath && data?.script
      ? `bash <(curl -Ls ${data.script}) --report '${window.location.origin}${record.reportPath}'`
      : '';
  const setName = setNameEdit;

  async function run(fn: () => Promise<{ success?: boolean } | undefined>) {
    setBusy(true);
    try {
      const msg = await fn();
      if (msg?.success) {
        onChanged();
        onClose();
      }
    } finally {
      setBusy(false);
    }
  }

  const save = () =>
    run(() =>
      HttpUtil.post(
        '/panel/api/nodes/dns/set',
        { nodeId: node?.id, name, ip, useAsAddress },
        JSON_HEADERS,
      ),
    );
  const sync = () =>
    run(() => HttpUtil.post(`/panel/api/nodes/dns/sync/${node?.id}`, { ip }, JSON_HEADERS));
  const unlink = () => run(() => HttpUtil.post(`/panel/api/nodes/dns/unlink/${node?.id}`));

  return (
    <Modal
      open={node !== null}
      title={t('pages.nodes.dns.modalTitle', { name: node?.name ?? '' })}
      onCancel={onClose}
      destroyOnHidden
      footer={
        <Space wrap>
          {record && (
            <Button danger onClick={unlink} loading={busy}>
              {t('pages.nodes.dns.unlink')}
            </Button>
          )}
          {record && (
            <Button onClick={sync} loading={busy}>
              {t('pages.nodes.dns.changeIp')}
            </Button>
          )}
          <Button type="primary" onClick={save} loading={busy} disabled={!configured}>
            {t('save')}
          </Button>
        </Space>
      }
    >
      <Spin spinning={loading}>
        {!configured && (
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 12 }}
            title={t('pages.nodes.dns.notConfigured')}
          />
        )}
        <Typography.Paragraph type="secondary">{t('pages.nodes.dns.intro')}</Typography.Paragraph>
        {record && (
          <Typography.Paragraph>
            {t('pages.nodes.dns.current', {
              name: record.name,
              ip: record.lastIp || '-',
              observed: record.observedIp || '-',
            })}
            {record.lastError && (
              <Typography.Text type="danger"> — {record.lastError}</Typography.Text>
            )}
          </Typography.Paragraph>
        )}
        {reporterCommand && (
          <Alert
            type="info"
            style={{ marginBottom: 12 }}
            title={t('pages.nodes.dns.reporter')}
            description={
              <Typography.Paragraph
                copyable={{ text: reporterCommand }}
                code
                style={{ wordBreak: 'break-all', marginBottom: 0 }}
              >
                {reporterCommand}
              </Typography.Paragraph>
            }
          />
        )}
        <Form layout="vertical">
          <Form.Item
            htmlFor={`${uid}-input-157`}
            label={t('pages.nodes.dns.name')}
            extra={t('pages.nodes.dns.nameDesc')}
          >
            <Input
              id={`${uid}-input-157`}
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="hk-a"
            />
          </Form.Item>
          <Form.Item
            htmlFor={`${uid}-input-160`}
            label={t('pages.nodes.dns.ip')}
            extra={t('pages.nodes.dns.ipDesc')}
          >
            <Input
              id={`${uid}-input-160`}
              value={ip}
              onChange={(e) => setIp(e.target.value.trim())}
              placeholder={record?.observedIp || '203.0.113.10'}
            />
          </Form.Item>
          <Checkbox checked={useAsAddress} onChange={(e) => setUseAsAddress(e.target.checked)}>
            {t('pages.nodes.dns.useAsAddress')}
          </Checkbox>
        </Form>
      </Spin>
    </Modal>
  );
}
