import { useTranslation } from 'react-i18next';
import { Alert, Button, Card, Space, Tag, Typography } from 'antd';
import { SafetyOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';
import { keys } from '@/api/queryKeys';

const API = '/panel/api/protect';
const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;

export interface FirewallRule {
  port: number;
  proto: 'tcp' | 'udp';
}

interface FirewallView {
  status: { tool: string; active: boolean };
  rules: FirewallRule[];
}

const firewallKey = () => [...keys.protect.root(), 'firewall'] as const;

export async function allowFirewall(rules: FirewallRule[]) {
  return HttpUtil.post<FirewallRule[]>(`${API}/firewall/allow`, { rules }, JSON_HEADERS);
}

// FirewallCard opens the ports of this server's inbounds in ufw, UDP included: a firewall that only
// lets 443/tcp through is the usual reason a Hysteria2 inbound gets no traffic.
export default function FirewallCard() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: firewallKey(),
    queryFn: async () => {
      const msg = await HttpUtil.get<FirewallView>(`${API}/firewall`, undefined, { silent: true });
      if (!msg?.success || !msg.obj) throw new Error(msg?.msg || 'firewall');
      return msg.obj;
    },
    retry: false,
  });
  const allow = useMutation({
    mutationFn: () => allowFirewall([]),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: firewallKey() }),
  });
  const view = query.data;
  return (
    <Card size="small" title={t('pages.protect.firewallTitle')}>
      <Typography.Paragraph type="secondary">
        {t('pages.protect.firewallDesc')}
      </Typography.Paragraph>
      {view && !view.status.active && (
        <Alert type="info" showIcon title={t('pages.protect.firewallInactive')} />
      )}
      {view?.status.active && (
        <Space orientation="vertical" size={8}>
          <Space wrap size={4}>
            {view.rules.map((r) => (
              <Tag key={`${r.port}/${r.proto}`} color={r.proto === 'udp' ? 'orange' : 'blue'}>
                {r.port}/{r.proto}
              </Tag>
            ))}
          </Space>
          <Button
            type="primary"
            icon={<SafetyOutlined />}
            loading={allow.isPending}
            disabled={view.rules.length === 0}
            onClick={() => allow.mutate()}
          >
            {t('pages.protect.firewallAllowAll')}
          </Button>
        </Space>
      )}
      <Typography.Paragraph
        type="secondary"
        style={{ fontSize: 12, marginTop: 8, marginBottom: 0 }}
      >
        {t('pages.protect.firewallCloud')}
      </Typography.Paragraph>
    </Card>
  );
}
