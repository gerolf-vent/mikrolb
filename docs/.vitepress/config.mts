import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'MikroLB',
  description: 'Kubernetes LoadBalancer controller for MikroTik RouterOS',
  cleanUrls: true,
  themeConfig: {
    nav: [
      { text: 'Guide', link: '/guide/getting-started' },
    ],
    sidebar: {
      '/guide/': [
        {
          text: 'Guide',
          items: [
            { text: 'Getting Started', link: '/guide/getting-started' },
            { text: 'Installation', link: '/guide/installation' },
            { text: 'IP Pools', link: '/guide/ip-pools' },
            { text: 'Services', link: '/guide/services' },
            { text: 'Debugging IPAllocations', link: '/guide/debugging-ipallocations' },
          ]
        }
      ]
    },
    socialLinks: [{ icon: 'github', link: 'https://github.com/gerolf-vent/mikrolb' }]
  }
})
