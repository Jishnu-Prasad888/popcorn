import { describe, expect, it } from 'vitest';

import { ApiError } from '../api/client';
import { describeFailure, formatBytes, isJunkFile, joinDest, parentOf } from './uploads';

const apiError = (status: number, message = 'x') =>
  new ApiError({ code: 'X', message, request_id: '' }, status);

describe('upload helpers', () => {
  it('joins a destination folder and a relative path', () => {
    expect(joinDest('', 'a.mkv')).toBe('a.mkv');
    expect(joinDest('/Movies/', 'Arrival/a.mkv')).toBe('Movies/Arrival/a.mkv');
    expect(joinDest(' Shows ', 'S1\\e01.mkv')).toBe('Shows/S1/e01.mkv');
  });

  it('finds the parent folder', () => {
    expect(parentOf('a.mkv')).toBe('');
    expect(parentOf('Shows/S1/e01.mkv')).toBe('Shows/S1');
  });

  it('skips OS clutter', () => {
    expect(isJunkFile('.DS_Store')).toBe(true);
    expect(isJunkFile('Thumbs.db')).toBe(true);
    expect(isJunkFile('._movie.mkv')).toBe(true);
    expect(isJunkFile('movie.mkv')).toBe(false);
  });

  it('formats sizes', () => {
    expect(formatBytes(512)).toBe('512 B');
    expect(formatBytes(1536)).toBe('1.5 KB');
    expect(formatBytes(2 * 1024 ** 3)).toBe('2.0 GB');
  });

  it('explains failures and says whether a retry can help', () => {
    expect(describeFailure(apiError(409), 1)).toEqual({
      message: 'A file with this name is already in that folder.',
      retryable: false,
    });
    expect(describeFailure(apiError(409, 'Library is offline.'), 1).retryable).toBe(true);
    expect(describeFailure(apiError(403), 1).retryable).toBe(false);
    expect(describeFailure(apiError(413), 2 * 1024 ** 3).message).toBe(
      'Larger than the 2.0 GB upload limit.',
    );
    expect(describeFailure(apiError(500), 1).retryable).toBe(true);
    expect(describeFailure(new Error('The connection to the server was lost.'), 1)).toEqual({
      message: 'The connection to the server was lost.',
      retryable: true,
    });
  });
});
