## matplotlib colormap data (viridis, magma, inferno, plasma, cividis)

The 256-entry lookup tables for the `viridis`, `magma`, `inferno`, `plasma`
and `cividis` color gradients embedded in `tiler/mutator/gradients.go` are
derived from the matplotlib colormap data.

The colormaps were created by Stéfan van der Walt and Nathaniel Smith
(viridis, magma, inferno, plasma) and by Jamie Nuñez, Christopher Anderton
and Ryan Renslow (cividis). The colormap data is released under the
CC0 "No Rights Reserved" public domain dedication:

```text
The colormap data is released under the CC0 license / public domain
dedication. See https://creativecommons.org/publicdomain/zero/1.0/ for the
full text of the dedication.
```

Source: https://github.com/matplotlib/matplotlib/blob/main/lib/matplotlib/_cm_listed.py

---

## Google Turbo colormap

The 256-entry lookup table for the `turbo` color gradient embedded in
`tiler/mutator/gradients.go` is derived from the Turbo colormap data.

```text
Copyright 2019 Google LLC.
SPDX-License-Identifier: Apache-2.0

Author: Anton Mikhailov

Licensed under the Apache License, Version 2.0 (the "License"); you may not
use this file except in compliance with the License. You may obtain a copy of
the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
License for the specific language governing permissions and limitations under
the License.
```

Source: https://gist.github.com/mikhailov-work/ee72ba4191942acecc03fe6da94fc73f

---

## matplotlib colormap data (terrain, gist_earth, coolwarm, seismic, cubehelix)

The color tables for the `terrain`, `gist-earth`, `coolwarm`, `seismic` and
`cubehelix` gradients embedded in `plugins/ramps/data.go` are sampled from the
matplotlib colormap implementations.

matplotlib is distributed under the matplotlib license, a BSD-style license
based on the PSF license, which permits commercial use, modification and
redistribution. Full text: https://matplotlib.org/stable/project/license.html

Source: https://github.com/matplotlib/matplotlib

---

## seaborn colormap data (mako, rocket)

The color tables for the `mako` and `rocket` gradients embedded in
`plugins/ramps/data.go` are derived from the seaborn colormap lookup tables.

```text
Copyright (c) 2012-2023, Michael L. Waskom
All rights reserved.

Redistributed under the BSD 3-Clause License. Full text:
https://github.com/mwaskom/seaborn/blob/master/LICENSE.md
```

Source: https://github.com/mwaskom/seaborn

---

## cmocean colormap data (haline, amp, balance, topo)

The color tables for the `haline`, `amp`, `balance` and `topo` gradients
embedded in `plugins/ramps/data.go` are derived from the cmocean colormap data.

```text
The MIT License (MIT)

Copyright (c) 2015 Kristen M. Thyng

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

Source: https://github.com/matplotlib/cmocean

---

## Scientific Colour Maps by Fabio Crameri (batlow, roma, berlin, nuuk, oleron)

The color tables for the `batlow`, `roma`, `berlin`, `nuuk` and `oleron`
gradients embedded in `plugins/ramps/data.go` are derived from the Scientific
Colour Maps by Fabio Crameri, obtained through the cmcrameri distribution.

```text
MIT License

Colormaps: Copyright (c) 2020 Fabio Crameri
Distribution and packaging: Copyright (c) 2020 Callum Rollo

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

Citation: Crameri, F. (2018). Scientific colour maps. Zenodo.
https://doi.org/10.5281/zenodo.1243862

Sources: https://www.fabiocrameri.ch/colourmaps/ and
https://github.com/callumrollo/cmcrameri

---

## ColorBrewer color specifications (YlGnBu, Blues, RdBu, BrBG, Spectral, PiYG, Dark2, Paired, Set2, Accent)

The color tables for the `ylgnbu`, `blues`, `rdbu`, `brbg`, `spectral`,
`piyg`, `dark2`, `paired`, `set2` and `accent` gradients embedded in
`plugins/ramps/data.go` use ColorBrewer color specifications.

```text
Apache-Style Software License for ColorBrewer software and ColorBrewer Color
Schemes

Copyright (c) 2002 Cynthia Brewer, Mark Harrower, and The Pennsylvania State
University.

Licensed under the Apache License, Version 2.0 (the "License"); you may not
use this file except in compliance with the License. You may obtain a copy of
the License at http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
License for the specific language governing permissions and limitations under
the License.

This product includes color specifications and designs developed by Cynthia
Brewer (http://colorbrewer2.org/).
```

Source: https://colorbrewer2.org/

---

## Esri LERC

The LERC (Limited Error Raster Compression) decoder in
`plugins/geotiff-colorizer` (`lerc.go`, `lercbits.go`, `lerchuffman.go`) is a
Go port of the decode paths of Esri's LERC C++ implementation, distributed
under the Apache License, Version 2.0. The full text of the Apache License,
Version 2.0 is reproduced elsewhere in this file and is available at
http://www.apache.org/licenses/LICENSE-2.0. The upstream NOTICE file reads:

```text
LERC
Copyright 2015-2026 Esri

This software embodiment is an implementation of

United States Patent 9,002,126, Limited Error Raster Compression (LERC).
Assignee: Esri.
Assignors/Inventors: Maurer, Thomas (Redlands, CA); Gao, Peng (Redlands, CA); Becker, Peter (Redlands, CA).

The right to practice this patent is hereby granted under the Apache V2.0 License Agreement,
Clause 3 - Grant of Patent License.

The license is available at
http://github.com/Esri/lerc/

For additional information, contact:

Environmental Systems Research Institute, Inc.
Attn: Contracts and Legal Department
380 New York Street
Redlands, CA 92373
E-mail: contracts@esri.com
```

Source: https://github.com/Esri/lerc

---

## meshoptimizer

The version 1 meshopt attribute encoder in `plugins/compression/meshopt_v1.go`,
used for `KHR_meshopt_compression` output, is a Go port of the vertex codec of
meshoptimizer (`src/vertexcodec.cpp`), including its mode selection
heuristics.

```text
MIT License

Copyright (c) 2016-2026 Arseny Kapoulkine

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

Source: https://github.com/zeux/meshoptimizer
