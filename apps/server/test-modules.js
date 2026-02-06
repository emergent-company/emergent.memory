process.env.UV_THREADPOOL_SIZE = '1';
console.log('Testing email-template.service dependencies...');

const modules = [
  '@nestjs/common',
  '@nestjs/typeorm',
  'typeorm',
  'fs',
  'path',
  'handlebars',
  'mjml',
];

for (const mod of modules) {
  try {
    console.log(`Loading ${mod}...`);
    require(mod);
    console.log(`  OK`);
  } catch (e) {
    console.log(`  FAIL: ${e.message}`);
  }
}

console.log('Done');
