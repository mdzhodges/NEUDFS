import grpc from 'k6/net/grpc';
import { check, sleep, group } from 'k6';
import { Counter, Trend } from 'k6/metrics';

const client = new grpc.Client();
client.load(['../proto'], 'service.proto');

const NLB_ADDR = __ENV.NLB_ADDR || 'localhost:50051';

// Custom metrics for Grafana
const cdLatency = new Trend('cd_latency', true);
const uploadLatency = new Trend('upload_latency', true);
const downloadLatency = new Trend('download_latency', true);
const deleteLatency = new Trend('delete_latency', true);
const rpcErrors = new Counter('rpc_errors');

// Preloaded student emails — update these from your seed data
const STUDENTS = JSON.parse(open('./students.json'));
const PROFESSOR = __ENV.PROFESSOR_EMAIL || 'noah.harris@northeastern.edu';

// ─────────────────────────────────────────────────
// Scenarios
// ─────────────────────────────────────────────────
export const options = {
  scenarios: {
    // Scenario 1: Steady classroom usage
    steady_state: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 10 },
        { duration: '2m', target: 30 },
        { duration: '1m', target: 30 },
        { duration: '30s', target: 0 },
      ],
      exec: 'studentWorkflow',
      tags: { scenario: 'steady_state' },
    },

    // Scenario 2: Burst — all students download at once
    burst_download: {
      executor: 'shared-iterations',
      vus: 30,
      iterations: 30,
      startTime: '4m30s',
      exec: 'burstDownload',
      tags: { scenario: 'burst_download' },
    },

    // Scenario 3: Teacher updates while students download
    teacher_update_race: {
      executor: 'per-vu-iterations',
      vus: 1,
      iterations: 1,
      startTime: '5m30s',
      exec: 'teacherUpdateRace',
      tags: { scenario: 'teacher_update' },
    },

    // Scenario 4: Scale test — ramp to 100 users
    scale_test: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '1m', target: 25 },
        { duration: '1m', target: 50 },
        { duration: '1m', target: 100 },
        { duration: '2m', target: 100 },
        { duration: '1m', target: 0 },
      ],
      startTime: '7m',
      exec: 'scaleWorkflow',
      tags: { scenario: 'scale_test' },
    },
  },

  thresholds: {
    'cd_latency': ['p(95)<100'],
    'upload_latency': ['p(95)<1000'],
    'download_latency': ['p(95)<500'],
    'rpc_errors': ['count<50'],
    'grpc_req_duration': ['p(95)<1000'],
  },
};

// ─────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────
function connect() {
  client.connect(NLB_ADDR, { plaintext: true, timeout: '10s' });
}

function meta(email) {
  return { metadata: { email: email } };
}

function navigate(email, folders) {
  // Reset to root first
  let resp = client.invoke('main.Server/ChangeDirectory', { folder: '' }, meta(email));
  cdLatency.add(resp.status === grpc.StatusOK ? resp.headers['grpc-status'] || 0 : 0);

  for (const folder of folders) {
    const start = Date.now();
    resp = client.invoke('main.Server/ChangeDirectory', { folder: folder }, meta(email));
    cdLatency.add(Date.now() - start);
    if (resp.status !== grpc.StatusOK) {
      rpcErrors.add(1);
      return false;
    }
  }
  return true;
}

function getStudentEmail() {
  return STUDENTS[(__VU - 1) % STUDENTS.length];
}

// ─────────────────────────────────────────────────
// Scenario 1: Normal student workflow
// cd → ls → cd → upload → download → repeat
// ─────────────────────────────────────────────────
export function studentWorkflow() {
  connect();
  const email = getStudentEmail();
  const studentName = email.split('@')[0].replace('.', '_');

  group('navigate to folder', () => {
    navigate(email, ['Khoury', 'CS5010', studentName]);
  });

  group('upload file', () => {
    const start = Date.now();
    const stream = new grpc.Stream(client, 'main.Server/Upload', meta(email));
    stream.write({
      request: {
        metadata: {
          name: `homework_${__VU}_${__ITER}.txt`,
          contentType: 'text/plain',
        },
      },
    });
    stream.write({
      request: {
        chunk: new Uint8Array(Array.from({ length: 1024 }, (_, i) => i % 256)),
      },
    });
    stream.end();
    const resp = stream.recv();
    uploadLatency.add(Date.now() - start);
    check(resp, { 'upload ok': (r) => r !== null });
    if (!resp) rpcErrors.add(1);
  });

  group('download file', () => {
    const start = Date.now();
    const resp = client.invoke(
      'main.Server/Download',
      { name: `homework_${__VU}_${__ITER}.txt` },
      meta(email)
    );
    downloadLatency.add(Date.now() - start);
    check(resp, { 'download ok': (r) => r && r.status === grpc.StatusOK });
    if (!resp || resp.status !== grpc.StatusOK) rpcErrors.add(1);
  });

  group('list directory', () => {
    const resp = client.invoke('main.Server/ListDirectory', {}, meta(email));
    check(resp, { 'ls ok': (r) => r && r.status === grpc.StatusOK });
  });

  sleep(Math.random() * 2 + 1); // 1-3s think time
  client.close();
}

// ─────────────────────────────────────────────────
// Scenario 2: All students download same file at once
// ─────────────────────────────────────────────────
export function burstDownload() {
  connect();
  const email = getStudentEmail();

  navigate(email, ['Khoury', 'CS5010', 'announcements']);

  const start = Date.now();
  const resp = client.invoke(
    'main.Server/Download',
    { name: 'lecture.txt' },
    meta(email)
  );
  downloadLatency.add(Date.now() - start);
  check(resp, { 'burst download ok': (r) => r && r.status === grpc.StatusOK });
  if (!resp || resp.status !== grpc.StatusOK) rpcErrors.add(1);

  client.close();
}

// ─────────────────────────────────────────────────
// Scenario 3: Teacher updates file while students download
// Spawns concurrent student downloads in background
// ─────────────────────────────────────────────────
export function teacherUpdateRace() {
  connect();

  // Teacher navigates and uploads initial file
  navigate(PROFESSOR, ['Khoury', 'CS5010', 'announcements']);

  const stream = new grpc.Stream(client, 'main.Server/Upload', meta(PROFESSOR));
  stream.write({
    request: {
      metadata: { name: 'race_test.txt', contentType: 'text/plain' },
    },
  });
  stream.write({
    request: {
      chunk: new Uint8Array(Array.from({ length: 10240 }, (_, i) => i % 256)),
    },
  });
  stream.end();
  stream.recv();

  // Teacher keeps overwriting
  for (let v = 2; v <= 5; v++) {
    const start = Date.now();
    const s = new grpc.Stream(client, 'main.Server/Upload', meta(PROFESSOR));
    s.write({
      request: {
        metadata: { name: 'race_test.txt', contentType: 'text/plain' },
      },
    });
    s.write({
      request: {
        chunk: new Uint8Array(
          Array.from({ length: 10240 }, () => v + 48)
        ),
      },
    });
    s.end();
    s.recv();
    uploadLatency.add(Date.now() - start);
    sleep(0.5);
  }

  // Teacher deletes
  const start = Date.now();
  const delResp = client.invoke(
    'main.Server/Delete',
    { path: 'race_test.txt' },
    meta(PROFESSOR)
  );
  deleteLatency.add(Date.now() - start);
  check(delResp, { 'delete ok': (r) => r && r.status === grpc.StatusOK });

  client.close();
}

// ─────────────────────────────────────────────────
// Scenario 4: Scale test — simple cd + ls at high concurrency
// Tests autoscaling behavior
// ─────────────────────────────────────────────────
export function scaleWorkflow() {
  connect();
  const email = getStudentEmail();

  const start = Date.now();
  navigate(email, ['Khoury', 'CS5010']);
  cdLatency.add(Date.now() - start);

  const resp = client.invoke('main.Server/ListDirectory', {}, meta(email));
  check(resp, { 'scale ls ok': (r) => r && r.status === grpc.StatusOK });
  if (!resp || resp.status !== grpc.StatusOK) rpcErrors.add(1);

  sleep(Math.random() * 1 + 0.5);
  client.close();
}