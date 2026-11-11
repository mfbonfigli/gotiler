// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightLinksValidator from 'starlight-links-validator';
import starlightLlmsTxt from 'starlight-llms-txt';

// https://astro.build/config
export default defineConfig({
	site: 'https://mfbonfigli.github.io',
	base: '/gotiler',
	integrations: [
		starlight({
			title: 'gotiler',
			description:
				'gotiler converts LAS, LAZ, E57 and PLY point clouds into streaming-ready OGC 3D Tiles 1.0 and 1.1, ready for CesiumJS and other 3D Tiles viewers.',
			// the gopher and the "gotiler" lettering of the banner, side by side
			logo: {
				light: './src/assets/logo-light.png',
				dark: './src/assets/logo-dark.png',
				alt: 'gotiler',
				replacesTitle: true,
			},
			// the emblem of the logo, also for phone home screens
			favicon: '/favicon.png',
			head: [{ tag: 'link', attrs: { rel: 'apple-touch-icon', href: '/gotiler/apple-touch-icon.png' } }],
			editLink: { baseUrl: 'https://github.com/mfbonfigli/gotiler/edit/v3/docs/' },
			customCss: ['./src/styles/theme.css'],
			components: {
				Hero: './src/components/Hero.astro',
				Footer: './src/components/Footer.astro',
				SocialIcons: './src/components/SocialIcons.astro',
			},
			// The point cloud and Gaussian splat guides, and the guides shared by
			// both under Reference, are listed by hand, so that each page keeps
			// its URL. The other groups list their folder's pages, ordered by
			// their sidebar.order.
			sidebar: [
				{ label: 'Getting started', items: [{ autogenerate: { directory: 'getting-started' } }] },
				{
					label: 'Point clouds',
					items: [
						'guides/input-formats',
						'guides/coordinate-systems',
						'guides/attributes',
						'guides/colorizing',
						'guides/output',
						'guides/examples',
					],
				},
				{
					label: 'Gaussian splats',
					items: [
						'guides/gaussian-splats/input-and-placement',
						'guides/gaussian-splats/size-and-detail',
						'guides/gaussian-splats/output',
						'guides/gaussian-splats/examples',
					],
				},
				{
					label: 'Reference',
					items: [
						'reference/cli-flags',
						'reference/color-ramps',
						'guides/viewer',
						'guides/placing-ungeoreferenced-clouds',
					],
				},
				{ label: 'Go library', items: [{ autogenerate: { directory: 'library' } }] },
				{ label: 'Project', items: [{ autogenerate: { directory: 'project' } }] },
			],
			plugins: [starlightLinksValidator(), starlightLlmsTxt()],
		}),
	],
});
