import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import * as plugin from '../plugin.js';

const fixture = JSON.parse(readFileSync(new URL('./contract-fixture.json', import.meta.url), 'utf8'));
for (const entry of fixture.cases) {
  test(entry.name, () => {
    let hook = plugin[entry.hook];
    for (const member of entry.path || []) hook = hook[member];
    if (entry.expectedError) {
      assert.throws(() => hook(...entry.args), error => error.message.includes(entry.expectedError));
    } else {
      assert.deepEqual(JSON.parse(JSON.stringify(hook(...entry.args))), entry.expected);
    }
  });
}

test('xAI proxy declares its own routes without claiming a legacy channel', () => {
  assert.equal(plugin.meta.key, 'xai-proxy');
  assert.equal(plugin.meta.auth, 'api_key');
  assert.equal(plugin.meta.channelTypes, undefined);
  assert.ok(plugin.meta.routes.every(route => route.path.startsWith('/xai-proxy/')));
  assert.deepEqual(plugin.meta.models, ['grok-imagine-video', 'grok-imagine-video-1.5', 'grok-imagine-video-1.5-lite']);
});

test('both deployed models preserve submitted resolution, duration, and usage', () => {
  for (const [model, resolutions] of [
    ['grok-imagine-video', ['480p', '720p']],
    ['grok-imagine-video-1.5', ['480p', '720p', '1080p']],
  ]) {
    for (const resolution of resolutions) {
      const ctx = {
        model, upstreamModel: model, baseUrl: 'https://proxy.example', apiKey: 'fixture-key',
        upstream: {kind: 'vendor'}, body: {kind: 'json', value: {model, prompt: 'A rolling ball', seconds: '1', resolution, aspect_ratio: '16:9'}},
      };
      const intent = plugin.protocols.openai_video.decodeRequest(ctx);
      const request = plugin.buildSubmitRequest({...ctx, ...intent});
      const facts = plugin.extractUsage({...ctx, ...intent});
      assert.equal(request.url, 'https://proxy.example/v1/videos/generations');
      assert.equal(request.body.duration, 1);
      assert.equal(request.body.resolution, resolution);
      assert.equal(request.body.aspect_ratio, '16:9');
      assert.equal(facts.seconds, 1);
      assert.equal(facts.resolution, resolution);
      assert.equal(facts.input_images, 0);
      assert.equal(facts.moderated, false);
      assert.equal('input_video_seconds' in facts, model === 'grok-imagine-video');
    }
  }
});

test('gateway upstream uses the new plugin native route', () => {
  const ctx = {model: 'grok-imagine-video', upstreamModel: 'grok-imagine-video', baseUrl: 'https://gateway.example', apiKey: 'fixture-key', upstream: {kind: 'new_api'}, requestBody: {prompt: 'a ball', duration: 1, resolution: '480p'}};
  assert.equal(plugin.buildSubmitRequest(ctx).url, 'https://gateway.example/xai-proxy/v1/videos/generations');
  assert.equal(plugin.buildQueryRequest({...ctx, taskId: 'task_123'}).url, 'https://gateway.example/xai-proxy/v1/videos/task_123');
});

function box(kind, ...contents) {
  const body = Buffer.concat(contents);
  const header = Buffer.alloc(8);
  header.writeUInt32BE(body.length + 8, 0);
  header.write(kind, 4, 4, 'ascii');
  return Buffer.concat([header, body]);
}

function mp4(width, height) {
  const fields = Buffer.alloc(4 + 72 + 8);
  fields.writeUInt16BE(width, 4 + 72);
  fields.writeUInt16BE(height, 4 + 72 + 4);
  return Buffer.concat([box('ftyp', Buffer.from('isom0000isom')), box('moov', box('trak', box('tkhd', fields)))]);
}

test('authenticated relative media is probed for actual edit resolution and billed duration', () => {
  const previous = globalThis.utils;
  try {
    for (const [width, height, resolution] of [[854, 480, '480p'], [1280, 720, '720p']]) {
      const requests = [];
      globalThis.utils = {fetch: request => { requests.push(request); return {status: 200, headers: {}, body: mp4(width, height)}; }};
      const ctx = {action: 'video_edit', taskId: 'upstream-id', baseUrl: 'https://proxy.example', apiKey: 'fixture-key'};
      const data = {status: 'done', video: {url: '/v1/videos/upstream-id/content', duration: 8, respect_moderation: true}};
      const result = plugin.parseTaskResult(ctx, data, {status: 200});
      assert.deepEqual(result, {status: 'SUCCESS', state: {output_resolution: resolution}});
      assert.deepEqual(plugin.extractUsageOnComplete(ctx, {plugin_state: result.state}, data), {resolution, seconds: 8, input_video_seconds: 8});
      assert.equal(requests.length, 1);
      assert.equal(requests[0].url, 'https://proxy.example/v1/videos/upstream-id/content');
      assert.equal(requests[0].headers.Authorization, 'Bearer fixture-key');
      assert.equal(requests[0].headers.Range, 'bytes=0-65535');
    }
  } finally { globalThis.utils = previous; }
});

test('invalid relative media never receives the credential during a billing probe', () => {
  const previous = globalThis.utils;
  let requests = 0;
  globalThis.utils = {fetch: () => { requests++; throw new Error('must not fetch'); }};
  try {
    const result = plugin.parseTaskResult({action: 'video_edit', taskId: 'upstream-id', baseUrl: 'https://proxy.example', apiKey: 'fixture-key'},
      {status: 'done', video: {url: '/v1/videos/other-id/content', duration: 8}}, {status: 200});
    assert.deepEqual(result, {status: 'SUCCESS'});
    assert.equal(requests, 0);
  } finally { globalThis.utils = previous; }
});
