console.log('1. Starting...');
console.log('UV_THREADPOOL_SIZE:', process.env.UV_THREADPOOL_SIZE);

console.log('2. Loading reflect-metadata...');
require('reflect-metadata');

console.log('3. Loading @nestjs/core...');
const { NestFactory } = require('@nestjs/core');

console.log('4. Loading app.module...');
const { AppModule } = require('./dist/modules/app.module');

console.log('5. All imports done!');
