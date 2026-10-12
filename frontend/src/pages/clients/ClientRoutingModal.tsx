import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Divider, Form, Modal, Radio, Spin } from 'antd';
import { useQuery, useQueryClient } from '@tanstack/react-query';

import RoutingChoiceEditor from '@/pages/rule-sets/RoutingChoiceEditor';
import TemplatePicker, { useTemplateGroups } from '@/pages/rule-sets/TemplatePicker';
import DnsEditor, { type DnsConfig, EMPTY_DNS } from '@/pages/rule-sets/DnsEditor';
import { type RoutingChoice, fetchRuleSets } from '@/pages/rule-sets/ruleSetApi';
import { keys } from '@/api/queryKeys';
import {
  type ClientRouting,
  clientRoutingKey,
  fetchClientRouting,
  saveClientRouting,
} from './clientRoutingApi';

interface Props {
  open: boolean;
  email: string;
  onClose: () => void;
}

function withDnsDefaults(dns: DnsConfig | null): DnsConfig {
  return dns
    ? { ...EMPTY_DNS, ...dns, domestic: dns.domestic ?? [], foreign: dns.foreign ?? [] }
    : EMPTY_DNS;
}

// One client's routing: its own rule sets, custom rules and DNS ahead of its group's and the global ones.
export default function ClientRoutingModal({ open, email, onClose }: Props) {
  const routing = useQuery({
    queryKey: clientRoutingKey(email),
    queryFn: () => fetchClientRouting(email),
    enabled: open && !!email,
  });
  if (!routing.data) {
    return (
      <Modal open={open} footer={null} onCancel={onClose} destroyOnHidden>
        {routing.error ? <Alert type="error" showIcon title={routing.error.message} /> : <Spin />}
      </Modal>
    );
  }
  return <RoutingEditor open={open} email={email} initial={routing.data} onClose={onClose} />;
}

function RoutingEditor({ open, email, initial, onClose }: Props & { initial: ClientRouting }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [own, setOwn] = useState(initial.own);
  const [template, setTemplate] = useState(initial.clashRules);
  const [choice, setChoice] = useState<RoutingChoice>({
    ruleSets: initial.ruleSets,
    extraRules: initial.extraRules,
  });
  const [ownDns, setOwnDns] = useState(initial.ownDns);
  const [dns, setDns] = useState<DnsConfig>(() => withDnsDefaults(initial.dns));
  const [saving, setSaving] = useState(false);

  const setsQuery = useQuery({
    queryKey: keys.ruleSets.list(),
    queryFn: fetchRuleSets,
    enabled: open && own,
  });
  const groupsQuery = useTemplateGroups(own ? template : '');

  const followLabel = initial.group
    ? t('pages.clients.routingFollowGroup', { group: initial.group })
    : t('pages.clients.routingFollowGlobal');

  async function confirm() {
    setSaving(true);
    try {
      const saved = await saveClientRouting({
        email,
        own,
        clashRules: own ? template.trim() : '',
        ruleSets: own ? choice.ruleSets : [],
        extraRules: own ? choice.extraRules : '',
        dns: ownDns ? dns : null,
      });
      if (!saved) return;
      queryClient.setQueryData(clientRoutingKey(email), saved);
      onClose();
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      open={open}
      title={t('pages.clients.routingTitle', { email })}
      okText={t('save')}
      cancelText={t('cancel')}
      confirmLoading={saving}
      onCancel={onClose}
      onOk={confirm}
      width={800}
      destroyOnHidden
    >
      <Alert
        type="info"
        showIcon
        title={t('pages.clients.routingDesc')}
        style={{ marginBottom: 16 }}
      />
      <Form layout="vertical">
        <Form.Item>
          <Radio.Group
            value={own ? 'own' : 'follow'}
            onChange={(e) => setOwn(e.target.value === 'own')}
            options={[
              { value: 'follow', label: followLabel },
              { value: 'own', label: t('pages.clients.routingOwn') },
            ]}
          />
        </Form.Item>
      </Form>
      {own && (
        <>
          <Alert
            type="warning"
            showIcon
            style={{ marginBottom: 12 }}
            title={t('pages.groups.clashRulesOwnDesc')}
          />
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
            sets={setsQuery.data ?? []}
            loading={setsQuery.isFetching}
            value={choice}
            onChange={setChoice}
            groups={groupsQuery.data ?? []}
          />
        </>
      )}
      <Divider titlePlacement="start">{t('pages.ruleSets.dnsTitle')}</Divider>
      <Radio.Group
        style={{ marginBottom: 12 }}
        value={ownDns ? 'own' : 'follow'}
        onChange={(e) => setOwnDns(e.target.value === 'own')}
        options={[
          { value: 'follow', label: t('pages.clients.routingDnsFollow') },
          { value: 'own', label: t('pages.clients.routingDnsOwn') },
        ]}
      />
      {ownDns && <DnsEditor value={dns} onChange={setDns} />}
    </Modal>
  );
}
