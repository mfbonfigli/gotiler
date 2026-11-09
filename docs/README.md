# gotiler Documentation Site

This folder builds the documentation site published at <https://mfbonfigli.github.io/gotiler/>, using [Starlight](https://starlight.astro.build/). Pages are Markdown or MDX files in `src/content/docs/`, and the home page's sections are components in `src/components/home/`.

With Node.js 22.23 or later:

```bash
npm ci          # install the exact dependencies from package-lock.json
npm run dev     # preview the site at http://localhost:4321/gotiler/
npm run build   # build the site into dist/, failing on broken internal links
```

Pushes to `v3` that change this folder publish the site through `.github/workflows/docs.yml`. Pull requests build it without publishing.
