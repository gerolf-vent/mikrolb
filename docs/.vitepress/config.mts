import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'MikroLB',
  description: 'Kubernetes LoadBalancer controller for MikroTik RouterOS',
  cleanUrls: true,
  themeConfig: {
    socialLinks: [{ icon: 'github', link: 'https://github.com/gerolf-vent/mikrolb' }]
  }
})
