import { useTranslation } from 'react-i18next';
import { Col, Form, Input, Row, Select, Switch } from 'antd';

import { HttpUtil } from '@/utils';

export interface DnsConfig {
  enable: boolean;
  mode: string;
  ipv6: boolean;
  domestic: string[];
  foreign: string[];
  foreignViaProxy: boolean;
  company: string;
  companyDirect: boolean;
}

export const EMPTY_DNS: DnsConfig = {
  enable: true,
  mode: 'fake-ip',
  ipv6: false,
  domestic: [],
  foreign: [],
  foreignViaProxy: false,
  company: '',
  companyDirect: true,
};

export async function fetchGlobalDns(): Promise<DnsConfig> {
  const msg = await HttpUtil.get<DnsConfig>('/panel/api/ruleSets/dns', undefined, { silent: true });
  if (!msg?.success || !msg.obj) throw new Error(msg?.msg || 'Failed to load DNS');
  return {
    ...EMPTY_DNS,
    ...msg.obj,
    domestic: msg.obj.domestic ?? [],
    foreign: msg.obj.foreign ?? [],
  };
}

const DOMESTIC = [
  'https://223.5.5.5/dns-query',
  'https://doh.pub/dns-query',
  '223.5.5.5',
  '119.29.29.29',
];
const FOREIGN = [
  'https://1.1.1.1/dns-query',
  'https://8.8.8.8/dns-query',
  'https://dns.google/dns-query',
];

interface Props {
  value: DnsConfig;
  onChange: (next: DnsConfig) => void;
}

// DnsEditor edits the DNS a profile gets: domestic/foreign resolvers and company DNS rules.
export default function DnsEditor({ value, onChange }: Props) {
  const { t } = useTranslation();
  const set = (patch: Partial<DnsConfig>) => onChange({ ...value, ...patch });
  return (
    <Form layout="vertical">
      <Form.Item label={t('pages.ruleSets.dnsEnable')} extra={t('pages.ruleSets.dnsEnableDesc')}>
        <Switch checked={value.enable} onChange={(enable) => set({ enable })} />
      </Form.Item>
      {value.enable && (
        <>
          <Form.Item
            label={t('pages.ruleSets.dnsCompany')}
            extra={t('pages.ruleSets.dnsCompanyDesc')}
          >
            <Input.TextArea
              value={value.company}
              onChange={(e) => set({ company: e.target.value })}
              autoSize={{ minRows: 3, maxRows: 10 }}
              placeholder={'corp.com, corp.local = 10.0.0.53, 10.0.0.54\nfactory.cn = 192.168.1.1'}
              style={{ fontFamily: 'monospace' }}
            />
          </Form.Item>
          <Form.Item>
            <Switch
              size="small"
              checked={value.companyDirect}
              onChange={(companyDirect) => set({ companyDirect })}
            />{' '}
            {t('pages.ruleSets.dnsCompanyDirect')}
          </Form.Item>
          <Row gutter={16}>
            <Col xs={24} md={12}>
              <Form.Item
                label={t('pages.ruleSets.dnsDomestic')}
                extra={t('pages.ruleSets.dnsDefaultHint')}
              >
                <Select
                  mode="tags"
                  value={value.domestic}
                  onChange={(domestic: string[]) => set({ domestic })}
                  options={DOMESTIC.map((v) => ({ value: v }))}
                  placeholder={DOMESTIC[0]}
                  tokenSeparators={[',', ' ']}
                />
              </Form.Item>
            </Col>
            <Col xs={24} md={12}>
              <Form.Item
                label={t('pages.ruleSets.dnsForeign')}
                extra={t('pages.ruleSets.dnsDefaultHint')}
              >
                <Select
                  mode="tags"
                  value={value.foreign}
                  onChange={(foreign: string[]) => set({ foreign })}
                  options={FOREIGN.map((v) => ({ value: v }))}
                  placeholder={FOREIGN[0]}
                  tokenSeparators={[',', ' ']}
                />
              </Form.Item>
            </Col>
          </Row>
          <Row gutter={16}>
            <Col xs={24} md={8}>
              <Form.Item label={t('pages.ruleSets.dnsMode')}>
                <Select
                  value={value.mode || 'fake-ip'}
                  onChange={(mode: string) => set({ mode })}
                  options={[
                    { value: 'fake-ip', label: 'fake-ip' },
                    { value: 'redir-host', label: 'redir-host' },
                  ]}
                />
              </Form.Item>
            </Col>
            <Col xs={24} md={8}>
              <Form.Item label={t('pages.ruleSets.dnsForeignViaProxy')}>
                <Switch
                  checked={value.foreignViaProxy}
                  onChange={(foreignViaProxy) => set({ foreignViaProxy })}
                />
              </Form.Item>
            </Col>
            <Col xs={24} md={8}>
              <Form.Item label="IPv6">
                <Switch checked={value.ipv6} onChange={(ipv6) => set({ ipv6 })} />
              </Form.Item>
            </Col>
          </Row>
        </>
      )}
    </Form>
  );
}
