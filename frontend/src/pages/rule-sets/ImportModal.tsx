import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Checkbox, Form, Input, Modal, Radio } from 'antd';

import { HttpUtil } from '@/utils';
import { JSON_HEADERS } from './ruleSetApi';

interface ImportResult {
  sets: string[];
  rules: number;
  skipped: number;
  dns: boolean;
}

interface Props {
  open: boolean;
  onClose: () => void;
  onImported: () => void;
}

// ImportModal turns an existing Clash/Mihomo config or subscription into rule sets (and its DNS).
export default function ImportModal({ open, onClose, onImported }: Props) {
  const { t } = useTranslation();
  const [source, setSource] = useState<'url' | 'paste'>('url');
  const [url, setUrl] = useState('');
  const [content, setContent] = useState('');
  const [importDns, setImportDns] = useState(false);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<ImportResult | null>(null);

  async function run() {
    setBusy(true);
    try {
      const body = source === 'url' ? { url, importDns } : { content, importDns };
      const msg = await HttpUtil.post<ImportResult>(
        '/panel/api/ruleSets/import',
        body,
        JSON_HEADERS,
      );
      if (msg?.success && msg.obj) {
        setResult(msg.obj);
        onImported();
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      open={open}
      title={t('pages.ruleSets.importTitle')}
      okText={t('pages.ruleSets.import')}
      cancelText={t('close')}
      confirmLoading={busy}
      okButtonProps={{ disabled: source === 'url' ? !url.trim() : !content.trim() }}
      onOk={run}
      onCancel={onClose}
      width={680}
      destroyOnHidden
    >
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 12 }}
        title={t('pages.ruleSets.importDesc')}
      />
      <Form layout="vertical">
        <Form.Item>
          <Radio.Group
            value={source}
            onChange={(e) => setSource(e.target.value)}
            options={[
              { value: 'url', label: t('pages.ruleSets.importFromUrl') },
              { value: 'paste', label: t('pages.ruleSets.importPaste') },
            ]}
          />
        </Form.Item>
        {source === 'url' ? (
          <Form.Item label={t('pages.ruleSets.importUrl')}>
            <Input
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder="https://example.com/clash/config.yaml"
            />
          </Form.Item>
        ) : (
          <Form.Item label={t('pages.ruleSets.importContent')}>
            <Input.TextArea
              value={content}
              onChange={(e) => setContent(e.target.value)}
              autoSize={{ minRows: 8, maxRows: 16 }}
              placeholder={
                'rules:\n  - DOMAIN-SUFFIX,corp.com,DIRECT\n  - DOMAIN-SUFFIX,maersk.com,PROXY'
              }
              style={{ fontFamily: 'monospace' }}
            />
          </Form.Item>
        )}
        <Form.Item>
          <Checkbox checked={importDns} onChange={(e) => setImportDns(e.target.checked)}>
            {t('pages.ruleSets.importDns')}
          </Checkbox>
        </Form.Item>
      </Form>
      {result && (
        <Alert
          type="success"
          showIcon
          title={t('pages.ruleSets.importResult', {
            sets: result.sets.length,
            rules: result.rules,
            skipped: result.skipped,
          })}
          description={
            <>
              {result.sets.join('、')}
              {result.dns && <div>{t('pages.ruleSets.importDnsDone')}</div>}
            </>
          }
        />
      )}
    </Modal>
  );
}
