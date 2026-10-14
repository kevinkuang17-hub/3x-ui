import { HttpUtil } from '@/utils';
import type { HysteriaMasquerade } from '@/schemas/protocols/stream/hysteria';

// decoyMasquerade turns the panel's deployed decoy into a Hysteria2 masquerade, so opening the
// address over HTTP/3 without the password shows the same website as the decoy page.
export async function decoyMasquerade(): Promise<HysteriaMasquerade> {
  const msg = await HttpUtil.get<string>('/panel/api/protect/decoy/page', undefined, {
    silent: true,
  });
  if (!msg?.success || typeof msg.obj !== 'string') {
    throw new Error(msg?.msg || 'decoy page');
  }
  return {
    type: 'string',
    dir: '',
    url: '',
    rewriteHost: false,
    insecure: false,
    content: msg.obj,
    headers: { 'Content-Type': 'text/html; charset=utf-8' },
    statusCode: 200,
  };
}
