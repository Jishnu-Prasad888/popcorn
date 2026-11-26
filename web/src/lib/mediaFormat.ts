import type { StreamInfo } from '../api/media';

function channels(n?: number, layout?: string) {
  if (layout && /\d\.\d/.test(layout)) return layout.match(/\d\.\d/)![0];
  if (!n) return '';
  return { 1: 'Mono', 2: 'Stereo', 6: '5.1', 8: '7.1' }[n] ?? `${n} ch`;
}

const CODEC_NAMES: Record<string, string> = {
  h264: 'H.264',
  hevc: 'HEVC',
  av1: 'AV1',
  vp9: 'VP9',
  mpeg4: 'MPEG-4',
  aac: 'AAC',
  ac3: 'Dolby Digital',
  eac3: 'Dolby Digital+',
  truehd: 'Dolby TrueHD',
  dts: 'DTS',
  flac: 'FLAC',
  opus: 'Opus',
  mp3: 'MP3',
  subrip: 'SRT',
  ass: 'ASS',
  ssa: 'SSA',
  webvtt: 'WebVTT',
  mov_text: 'MP4 text',
  hdmv_pgs_subtitle: 'PGS',
  dvd_subtitle: 'VobSub',
};

export const codecName = (c?: string) => (c ? (CODEC_NAMES[c] ?? c.toUpperCase()) : '');

export function audioLabel(s: StreamInfo): string {
  return [
    s.language_name || 'Unknown',
    codecName(s.codec),
    channels(s.channels, s.channel_layout),
    s.title && !/^(stereo|surround|5\.1)$/i.test(s.title) ? s.title : '',
  ]
    .filter(Boolean)
    .join('  ');
}
