// The datasets of the demo, served from the same CloudFront distribution as
// this page. The paths start at the site root, so a copy of the page in a
// subfolder, such as a beta, finds them too. The page lists only the
// datasets whose tileset.json is there, so a dataset can be added here
// before it is uploaded.
//
// kind is 'points' or 'splats'. Splat scenes have no color or point
// settings: CesiumJS draws them as they are. terrain shows Cesium World
// Terrain under the dataset instead of the flat globe. maximumScreenSpaceError
// is the starting level of detail.

// the credit of the Lublin scene
const teleportour =
  '3D scanning data created and provided by <a href="https://www.linkedin.com/in/andrii-shramko/" target="_blank" rel="noopener">Andrii Shramko</a>, <a href="https://www.linkedin.com/company/teleportour/" target="_blank" rel="noopener">Teleportour</a> · <a href="https://teleportour.com" target="_blank" rel="noopener">teleportour.com</a>';

export const datasets = [
  {
    id: 'helsinki',
    name: 'Helsinki',
    summary: '313M points · classified',
    kind: 'points',
    url: '/helsinki-1.1-v3/tileset.json',
    credit:
      'Point cloud: <a href="https://doi.org/10.5281/zenodo.5578198" target="_blank" rel="noopener">Julin, A., &amp; Rantanen, T. (2021)</a>, CC BY 4.0',
    maximumScreenSpaceError: 10,
    // The classes the points use. The dataset documents no class list, so
    // only the standard ASPRS codes get a name.
    classes: [
      { code: 1, name: 'Unclassified', color: '#9aa3ad' },
      { code: 2, name: 'Ground', color: '#a0662b' },
      { code: 3, name: 'Low vegetation', color: '#9be15d' },
      { code: 5, name: 'High vegetation', color: '#2e9e45' },
      { code: 6, name: 'Building', color: '#f05a3c' },
      { code: 13, name: 'Class 13', color: '#8a6cf0' },
      { code: 18, name: 'High noise', color: '#ff5fb0' },
      { code: 19, name: 'Class 19', color: '#3cc8e6' },
    ],
  },
  {
    id: 'melbourne',
    name: 'Melbourne',
    summary: '350M points',
    kind: 'points',
    url: '/melbourne-1.1-v3/tileset.json',
    credit:
      'Point cloud: <a href="https://data.melbourne.vic.gov.au/explore/dataset/city-of-melbourne-3d-point-cloud-2018/information/" target="_blank" rel="noopener">City of Melbourne</a>, CC BY 4.0',
    maximumScreenSpaceError: 10,
    // every point is unclassified
    classes: [],
  },
  {
    id: 'lublin',
    name: 'Lublin, Poland',
    summary: '259M Gaussian splats',
    kind: 'splats',
    url: '/lublin-splats/tileset.json',
    credit: teleportour,
    maximumScreenSpaceError: 26,
    terrain: true,
  },
];
