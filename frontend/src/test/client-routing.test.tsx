import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';

import ClientRoutingField from '@/pages/clients/ClientRoutingField';
import { HttpUtil, Msg } from '@/utils';

import { renderWithProviders } from './test-utils';

const getSpy = vi.mocked(HttpUtil.get);
const postSpy = vi.mocked(HttpUtil.post);

function routing(over: Record<string, unknown> = {}) {
  return {
    email: 'alice',
    group: 'vip',
    own: true,
    clashRules: '',
    ruleSets: [{ id: 3, policy: 'DIRECT' }],
    extraRules: 'DOMAIN-SUFFIX,corp.example,DIRECT',
    ownDns: false,
    dns: null,
    source: 'client',
    ...over,
  };
}

function mockServer(view: Record<string, unknown>) {
  getSpy.mockImplementation(async (url: string) => {
    if (url.startsWith('/panel/api/ruleSets/client')) return new Msg(true, '', view);
    if (url === '/panel/api/ruleSets/list')
      return new Msg(true, '', [
        { id: 3, name: 'Banks', category: 'finance', rules: 'bank.example' },
      ]);
    return new Msg(true, '', {});
  });
  postSpy.mockImplementation(
    async (_url: string, body?: unknown) =>
      new Msg(true, '', { ...view, ...(body as object), source: 'group' }),
  );
}

const savedBody = () =>
  postSpy.mock.calls.find((c) => c[0] === '/panel/api/ruleSets/client')?.[1] as
    | Record<string, unknown>
    | undefined;

beforeEach(() => {
  getSpy.mockReset();
  postSpy.mockReset();
});

describe('ClientRoutingField', () => {
  it('shows whose rules the subscription uses', async () => {
    mockServer(routing({ own: false, ruleSets: [], extraRules: '', source: 'group' }));
    renderWithProviders(<ClientRoutingField email="alice" />);
    expect(await screen.findByText("Group vip's rules")).toBeTruthy();
    expect(getSpy.mock.calls[0][0]).toBe('/panel/api/ruleSets/client?email=alice');
  });

  it('saves the client back to following its group without its own picks', async () => {
    mockServer(routing());
    renderWithProviders(<ClientRoutingField email="alice" />);
    await screen.findByText('Own rules');
    fireEvent.click(screen.getByRole('button', { name: /Set routing/ }));
    fireEvent.click(await screen.findByText('Follow group vip'));
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(savedBody()).toBeTruthy());
    expect(savedBody()).toEqual({
      email: 'alice',
      own: false,
      clashRules: '',
      ruleSets: [],
      extraRules: '',
      dns: null,
    });
  });

  it('keeps the client own picks and sends its own DNS when chosen', async () => {
    mockServer(routing());
    renderWithProviders(<ClientRoutingField email="alice" />);
    await screen.findByText('Own rules');
    fireEvent.click(screen.getByRole('button', { name: /Set routing/ }));
    fireEvent.click(await screen.findByText("This client's own DNS (e.g. its company DNS)"));
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(savedBody()).toBeTruthy());
    const body = savedBody()!;
    expect(body.own).toBe(true);
    expect(body.ruleSets).toEqual([{ id: 3, policy: 'DIRECT' }]);
    expect(body.extraRules).toBe('DOMAIN-SUFFIX,corp.example,DIRECT');
    expect((body.dns as { enable: boolean }).enable).toBe(true);
  });
});
