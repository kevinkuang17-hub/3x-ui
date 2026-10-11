import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Card,
  Col,
  Divider,
  Form,
  Input,
  Modal,
  Row,
  Space,
  Tag,
  Typography,
} from 'antd';
import {
  CheckCircleFilled,
  EyeOutlined,
  PoweroffOutlined,
  RocketOutlined,
  UploadOutlined,
} from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';
import { keys } from '@/api/queryKeys';
import FrontSection, { publicSiteURL, useFront } from './FrontSection';

const API = '/panel/api/protect';
const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;

interface DecoySettings {
  mode: string;
  title: string;
  tagline: string;
  email: string;
  modes: string[];
  customSite: boolean;
  secretPath: boolean;
}

interface SiteDraft {
  mode: string;
  title: string;
  tagline: string;
  email: string;
}

// Templates that show the site name; the nginx page is a bare server page without one.
const NAMED = new Set(['company', 'logistics', 'tech', 'blog', 'photo', 'maintenance']);

const decoyKey = () => [...keys.protect.root(), 'decoy'] as const;

export default function DecoyCard() {
  const { t } = useTranslation();
  const query = useQuery({
    queryKey: decoyKey(),
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const msg = await HttpUtil.get<DecoySettings>(`${API}/decoy`, undefined, { silent: true });
      if (!msg?.success || !msg.obj) throw new Error(msg?.msg || 'request failed');
      return msg.obj;
    },
  });
  return (
    <Card size="small" title={t('pages.protect.decoyTitle')}>
      <Typography.Paragraph type="secondary">{t('pages.protect.decoyDesc')}</Typography.Paragraph>
      {query.data && <DecoyForm key={query.dataUpdatedAt} initial={query.data} />}
      <Divider />
      <FrontSection />
    </Card>
  );
}

function DecoyForm({ initial }: { initial: DecoySettings }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<SiteDraft>({
    mode: initial.mode === 'off' ? 'company' : initial.mode,
    title: initial.title,
    tagline: initial.tagline,
    email: initial.email,
  });
  const [preview, setPreview] = useState<string | null>(null);
  const siteURL = publicSiteURL(useFront().data);
  // Turning off or switching to the uploaded site keeps the deployed details, not half-typed ones.
  const saved: SiteDraft = {
    mode: initial.mode,
    title: initial.title,
    tagline: initial.tagline,
    email: initial.email,
  };
  const [uploading, setUploading] = useState(false);
  const refresh = () => queryClient.invalidateQueries({ queryKey: decoyKey() });

  const deploy = useMutation({
    mutationFn: (body: SiteDraft) => HttpUtil.post(`${API}/decoy`, body, JSON_HEADERS),
    onSuccess: (msg) => {
      if (msg?.success) refresh();
    },
  });
  const previewMut = useMutation({
    mutationFn: (body: SiteDraft) =>
      HttpUtil.post<string>(`${API}/decoy/preview`, body, { ...JSON_HEADERS, silentSuccess: true }),
    onSuccess: (msg) => {
      if (msg?.success) setPreview(msg.obj ?? '');
    },
  });

  const needsName = NAMED.has(draft.mode);
  const canDeploy = !needsName || draft.title.trim() !== '';
  const templates = initial.modes.filter((m) => m !== 'off' && m !== 'custom');

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
        if (msg?.success) refresh();
      } finally {
        setUploading(false);
      }
    });
    input.click();
  }

  return (
    <>
      {!initial.secretPath && (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 12 }}
          title={t('pages.protect.decoyNeedsPath')}
        />
      )}
      {initial.mode !== 'off' ? (
        <Alert
          type="success"
          showIcon
          style={{ marginBottom: 16 }}
          title={t('pages.protect.decoyStatusOn', {
            type: t(`pages.protect.decoyMode_${initial.mode}`),
            name: initial.title || '—',
          })}
          action={
            <Space wrap>
              <Button size="small" href={siteURL} target="_blank" rel="noreferrer">
                {t('pages.protect.decoyOpen')}
              </Button>
              <Button
                size="small"
                danger
                icon={<PoweroffOutlined />}
                loading={deploy.isPending && deploy.variables?.mode === 'off'}
                onClick={() => deploy.mutate({ ...saved, mode: 'off' })}
              >
                {t('pages.protect.decoyTurnOff')}
              </Button>
            </Space>
          }
        />
      ) : (
        <Alert
          type="info"
          showIcon
          style={{ marginBottom: 16 }}
          title={t('pages.protect.decoyStatusOff')}
        />
      )}

      <Typography.Text strong>{t('pages.protect.decoyChoose')}</Typography.Text>
      <Row gutter={[12, 12]} style={{ margin: '8px 0 16px' }}>
        {templates.map((m) => {
          const selected = draft.mode === m;
          return (
            <Col key={m} xs={24} sm={12} lg={8} xl={6}>
              <Card
                size="small"
                hoverable
                onClick={() => setDraft({ ...draft, mode: m })}
                style={{
                  height: '100%',
                  borderColor: selected ? 'var(--ant-color-primary, #1677ff)' : undefined,
                  borderWidth: selected ? 2 : 1,
                }}
              >
                <Space size={6}>
                  {selected && (
                    <CheckCircleFilled style={{ color: 'var(--ant-color-primary, #1677ff)' }} />
                  )}
                  <Typography.Text strong>{t(`pages.protect.decoyMode_${m}`)}</Typography.Text>
                  {initial.mode === m && <Tag color="green">{t('pages.protect.decoyLive')}</Tag>}
                </Space>
                <Typography.Paragraph type="secondary" style={{ margin: '6px 0 0', fontSize: 13 }}>
                  {t(`pages.protect.decoyModeDesc_${m}`)}
                </Typography.Paragraph>
              </Card>
            </Col>
          );
        })}
      </Row>

      <Form layout="vertical">
        <Row gutter={16}>
          <Col xs={24} md={8}>
            <Form.Item
              label={t('pages.protect.decoySiteTitle')}
              required={needsName}
              extra={needsName ? undefined : t('pages.protect.decoyNoName')}
            >
              <Input
                value={draft.title}
                maxLength={80}
                disabled={!needsName}
                placeholder={t('pages.protect.decoySiteTitlePlaceholder')}
                onChange={(e) => setDraft({ ...draft, title: e.target.value })}
              />
            </Form.Item>
          </Col>
          <Col xs={24} md={10}>
            <Form.Item label={t('pages.protect.decoyTagline')}>
              <Input
                value={draft.tagline}
                maxLength={200}
                disabled={!needsName}
                placeholder={t('pages.protect.decoyTaglinePlaceholder')}
                onChange={(e) => setDraft({ ...draft, tagline: e.target.value })}
              />
            </Form.Item>
          </Col>
          <Col xs={24} md={6}>
            <Form.Item label={t('pages.protect.decoyEmail')}>
              <Input
                value={draft.email}
                disabled={!needsName}
                placeholder="info@example.com"
                onChange={(e) => setDraft({ ...draft, email: e.target.value })}
              />
            </Form.Item>
          </Col>
        </Row>
        <Space wrap>
          <Button
            type="primary"
            icon={<RocketOutlined />}
            disabled={!canDeploy}
            loading={deploy.isPending && deploy.variables?.mode !== 'off'}
            onClick={() => deploy.mutate(draft)}
          >
            {t('pages.protect.decoyDeploy')}
          </Button>
          <Button
            icon={<EyeOutlined />}
            loading={previewMut.isPending}
            onClick={() => previewMut.mutate(draft)}
          >
            {t('pages.protect.decoyPreview')}
          </Button>
          {!canDeploy && (
            <Typography.Text type="warning">{t('pages.protect.decoyNameRequired')}</Typography.Text>
          )}
        </Space>
      </Form>

      <div style={{ marginTop: 20 }}>
        <Typography.Text strong>{t('pages.protect.decoyCustomTitle')}</Typography.Text>
        <Typography.Paragraph type="secondary" style={{ margin: '4px 0 8px' }}>
          {t('pages.protect.decoyUploadDesc')}
        </Typography.Paragraph>
        <Space wrap>
          <Button icon={<UploadOutlined />} loading={uploading} onClick={upload}>
            {t('pages.protect.decoyUpload')}
          </Button>
          <Button
            disabled={!initial.customSite}
            icon={<RocketOutlined />}
            loading={deploy.isPending && deploy.variables?.mode === 'custom'}
            onClick={() => deploy.mutate({ ...saved, mode: 'custom' })}
          >
            {t('pages.protect.decoyDeployCustom')}
          </Button>
        </Space>
      </div>

      <Modal
        open={preview !== null}
        title={t('pages.protect.decoyPreviewTitle')}
        footer={null}
        width={1000}
        onCancel={() => setPreview(null)}
        destroyOnHidden
      >
        <iframe
          title={t('pages.protect.decoyPreviewTitle')}
          srcDoc={preview ?? ''}
          sandbox=""
          style={{
            width: '100%',
            height: '70vh',
            border: '1px solid rgba(128,128,128,.3)',
            borderRadius: 6,
            background: '#fff',
          }}
        />
      </Modal>
    </>
  );
}
