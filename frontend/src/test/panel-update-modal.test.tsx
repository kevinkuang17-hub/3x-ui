import { fireEvent, screen } from '@testing-library/react';
import { Modal } from 'antd';
import { afterEach, describe, expect, it, vi } from 'vitest';

import PanelUpdateModal from '@/pages/index/PanelUpdateModal';
import { HttpUtil } from '@/utils';
import { renderWithProviders } from './test-utils';

let restoreUseModal = () => {};

describe('panel update modal', () => {
  afterEach(() => {
    restoreUseModal();
    restoreUseModal = () => {};
    vi.mocked(HttpUtil.post).mockReset();
  });

  it('lets the confirmation close before the update request finishes', () => {
    const confirm = vi.fn();
    const useModal = vi.spyOn(Modal, 'useModal').mockReturnValue([
      {
        confirm,
        error: vi.fn(),
        warning: vi.fn(),
      } as unknown as ReturnType<typeof Modal.useModal>[0],
      <span key="modal-context" />,
    ]);
    restoreUseModal = () => useModal.mockRestore();
    vi.mocked(HttpUtil.post).mockReturnValue(new Promise(() => {}));
    const onClose = vi.fn();
    const onBusy = vi.fn();
    renderWithProviders(
      <PanelUpdateModal
        open
        info={{
          channel: 'dev',
          currentVersion: 'dev+old',
          latestVersion: 'dev+new',
          currentCommit: 'old',
          latestCommit: 'new',
          updateAvailable: true,
        }}
        devChannelEnable
        onClose={onClose}
        onBusy={onBusy}
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: /Update Panel/ }));
    expect(confirm).toHaveBeenCalledOnce();
    const config = confirm.mock.calls[0]?.[0];
    if (!config?.onOk) throw new Error('Confirmation onOk handler was not registered');

    expect(config.onOk()).toBeUndefined();
    expect(onClose).toHaveBeenCalledOnce();
    expect(HttpUtil.post).toHaveBeenCalledWith('/panel/api/server/updatePanel');
    expect(onBusy).toHaveBeenCalledWith({
      busy: true,
      tip: 'Installation is in progress, please do not refresh this page (dev+new)',
    });
  });

  it('warns when official 3X-UI is ahead of every release of this build', () => {
    const info = {
      channel: 'stable',
      currentVersion: '3.9.0-k12',
      latestVersion: 'v3.9.0-k12',
      updateAvailable: false,
      upstreamVersion: 'v3.10.0',
    };
    const { rerender } = renderWithProviders(
      <PanelUpdateModal
        open
        info={{ ...info, upstreamPending: true }}
        onClose={vi.fn()}
        onBusy={vi.fn()}
      />,
    );
    expect(
      screen.getByText('Official 3X-UI v3.10.0 is out but not in this build yet'),
    ).toBeTruthy();

    rerender(
      <PanelUpdateModal
        open
        info={{ ...info, upstreamPending: false }}
        onClose={vi.fn()}
        onBusy={vi.fn()}
      />,
    );
    expect(screen.queryByText(/is out but not in this build yet/)).toBeNull();
  });
});

describe('panel update modal: automatic update', () => {
  it('loads the setting and saves the switch with the chosen hour and nodes', async () => {
    const { Msg } = await import('@/utils');
    const { waitFor } = await import('@testing-library/react');
    const off = { enable: false, hour: 4, nodes: true, lastRun: '', lastResult: '' };
    vi.mocked(HttpUtil.get).mockResolvedValue(new Msg(true, '', off));
    vi.mocked(HttpUtil.post).mockResolvedValue(new Msg(true, '', { ...off, enable: true }));
    renderWithProviders(
      <PanelUpdateModal
        open
        info={{ currentVersion: '3.10.2-k2', latestVersion: 'v3.10.2-k2', updateAvailable: false }}
        onClose={vi.fn()}
        onBusy={vi.fn()}
      />,
    );
    const label = await screen.findByText('Update automatically');
    const toggle = label.parentElement!.querySelector('button[role="switch"]')!;
    fireEvent.click(toggle);
    await waitFor(() => expect(vi.mocked(HttpUtil.post)).toHaveBeenCalled());
    expect(vi.mocked(HttpUtil.post).mock.calls[0].slice(0, 2)).toEqual([
      '/panel/api/server/autoUpdate',
      { enable: true, hour: 4, nodes: true },
    ]);
    expect(await screen.findByText('Every day at (Beijing time)')).toBeTruthy();
  });
});
