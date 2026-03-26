import grpc from 'k6/net/grpc';
import { check, sleep } from 'k6';
import exec from 'k6/execution';

const client = new grpc.Client();
const PROTO_PATH = '../proto/server.proto'; 

client.load(['.'], PROTO_PATH);

export const options = {
  stages: [
    { duration: '10s', target: 3 }, // Capped at 3 to match your 3 seeded users
    { duration: '30s', target: 3 }, 
    { duration: '10s', target: 0 },  
  ],
};

// Define your seeded users
const seededEmails = [
  'alice@school.edu',
  'bob@school.edu',
  'professor@school.edu'
];

export default () => {
  client.connect('localhost:50051', { plaintext: true });

  // Map the Virtual User ID to one of the 3 emails
  const userIndex = (exec.vu.idInTest - 1) % seededEmails.length;
  const userEmail = seededEmails[userIndex]; 
  const params = { metadata: { 'email': userEmail } };

  for (let i = 0; i < 50; i++) {
    client.invoke('main.Server/ChangeDirectory', { folder: '' }, params);

    // Check current directory
    const pwdRes = client.invoke('main.Server/CurrentDirectory', {}, params);
    check(pwdRes, { 'pwd OK': (r) => r && r.status === grpc.StatusOK });

    // CD into College
    const cdColRes = client.invoke('main.Server/ChangeDirectory', { folder: 'Khoury' }, params);
    check(cdColRes, { 'cd Khoury OK': (r) => r && r.status === grpc.StatusOK });

    // CD into Class
    const cdClassRes = client.invoke('main.Server/ChangeDirectory', { folder: 'CS101' }, params);
    check(cdClassRes, { 'cd CS101 OK': (r) => r && r.status === grpc.StatusOK });

    // List Directory contents
    const lsRes = client.invoke('main.Server/ListDirectory', {}, params);
    check(lsRes, { 'ls OK': (r) => r && r.status === grpc.StatusOK });

    // CD back up one level
    const cdUpRes = client.invoke('main.Server/ChangeDirectory', { folder: '..' }, params);
    check(cdUpRes, { 'cd .. OK': (r) => r && r.status === grpc.StatusOK });

    // Reset to root so the next loop iteration starts clean
    const resetRes = client.invoke('main.Server/ChangeDirectory', { folder: '' }, params);
    check(resetRes, { 'reset to root OK': (r) => r && r.status === grpc.StatusOK });

    sleep(0.5); 
  }

  client.close();
};