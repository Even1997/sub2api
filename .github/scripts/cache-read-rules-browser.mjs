import assert from 'node:assert/strict'
import fs from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { createRequire } from 'node:module'

const root = process.cwd()
const frontend = path.join(root, 'frontend')
const requireFrontend = createRequire(path.join(frontend, 'package.json'))
const { createServer } = requireFrontend('vite')
const vue = requireFrontend('@vitejs/plugin-vue').default
process.chdir(frontend)
const html = `<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"></head><body><div id="app"></div><script type="module">
import {createApp,ref} from 'vue';
import Editor from '/src/components/admin/group/CacheReadReductionRulesEditor.vue';
import {createI18n} from 'vue-i18n';
import zh from '/src/i18n/locales/zh/admin/overview.ts';
import en from '/src/i18n/locales/en/admin/overview.ts';
import '/src/style.css';
const locale=new URLSearchParams(location.search).get('lang')||'zh';
const app={components:{Editor},setup(){return{rules:ref([{model:'gpt-6.1-sol',percent:5,start_hit_rate_percent:80},{model:'gpt-6-astra',percent:3}])}},template:'<main class="mx-auto max-w-3xl p-4 sm:p-8"><form @submit.prevent><Editor v-model="rules"/></form></main>'};
createApp(app).use(createI18n({legacy:false,locale,messages:{zh:{admin:zh},en:{admin:en}}})).mount('#app');
</script></body></html>`
const server = await createServer({
  root: frontend, configFile: false, envFile: false, cacheDir: path.join(os.tmpdir(),'sub2api-cache-rule-vite'),
  plugins: [vue(), {name:'cache-rule-preview',configureServer(vite){vite.middlewares.use((req,res,next)=>{
    if(req.url?.split('?')[0]!=='/')return next()
    res.setHeader('Content-Type','text/html')
    void vite.transformIndexHtml('/',html).then(value=>res.end(value))
  })}}],
  resolve:{alias:{'@':path.join(frontend,'src'), 'vue':'vue/dist/vue.esm-bundler.js','vue-i18n':'vue-i18n/dist/vue-i18n.esm-bundler.js'}},
  server:{host:'127.0.0.1',port:4197,strictPort:true},
  optimizeDeps:{noDiscovery:true,include:[]},
})
await server.listen()
console.log('Preview: http://127.0.0.1:4197')
if (!process.argv.includes('--serve')) {
  const requireBrowser=createRequire(path.join(process.env.PLAYWRIGHT_ROOT,'package.json'))
  const {chromium}=requireBrowser('playwright')
  const browser=await chromium.launch({headless:true})
  try {
    const out=process.env.CACHE_RULE_SCREENSHOTS||path.join(os.tmpdir(),'sub2api-cache-rules')
    await fs.mkdir(out,{recursive:true})
    for(const width of [390,1440])for(const lang of ['zh','en']){
      const page=await browser.newPage({viewport:{width,height:1000}})
      const errors=[]
      page.on('pageerror',error=>errors.push(error.message))
      await page.goto('http://127.0.0.1:4197/?lang='+lang)
      await page.getByText('4.00%',{exact:false}).waitFor()
      assert.equal(await page.locator('section').evaluate(el=>getComputedStyle(el).borderTopWidth),'1px')
      assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
      await page.locator('select').first().selectOption('fixed')
      await page.getByText('5.00%',{exact:false}).waitFor()
      await page.locator('select').first().selectOption('dynamic')
      await page.getByText('4.00%',{exact:false}).waitFor()
      assert.deepEqual(errors,[])
      await page.screenshot({path:path.join(out,`rules-${lang}-${width}.png`),fullPage:true})
      await page.close()
    }
    console.log('Browser checks passed; screenshots: '+out)
  }finally{await browser.close();await server.close()}
}
