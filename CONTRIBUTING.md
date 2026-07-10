# Contribute to E-MOBI GPU Operator

## Introduction

E-MOBI / EKONOMIK MOBIL, S.R.L. welcomes contributions to the E-MOBI GPU Operator project. This is a customized, community-driven fork of the NVIDIA GPU Operator, optimized for AI-powered solutions, autonomous systems, and high-performance computing.

Kubernetes provides access to special hardware resources such as GPUs, NICs, Infiniband adapters and other devices through the device plugin [framework](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/device-plugins/). Managing these resources in Kubernetes is complex and error-prone. GPUs and NICs require special drivers, plugins, runtimes, and libraries to ensure fully high-performance functionality.

![e-mobi-robotics](https://img.shields.io/badge/E--MOBI%20Robotics-Community%20Driven-brightgreen?style=for-the-badge)

The E-MOBI GPU Operator automates the management of all GPU software components needed to provision accelerated computing resources. It leverages the standard [Operator Framework](https://cloud.redhat.com/blog/introducing-the-operator-framework) within Kubernetes.

The E-MOBI GPU Operator is a community-maintained, open-source product with enhancements and customizations by E-MOBI / EKONOMIK MOBIL, S.R.L. It respects all original licensing and contributions from NVIDIA and the broader community.

---

## Architecture

The E-MOBI GPU Operator is built upon the NVIDIA GPU Operator architecture with E-MOBI enhancements:

- **Base**: NVIDIA GPU Operator (Apache 2.0 Licensed)
- **Enhancements**: E-MOBI customizations for AI/robotics
- **Components**:
  - [gpu-operator](https://github.com/NVIDIA/gpu-operator) - CRD and controller logic
  - [k8s-device-plugin](https://github.com/NVIDIA/k8s-device-plugin) - NVIDIA Device Plugin for Kubernetes
  - [gpu-driver-container](https://github.com/NVIDIA/gpu-driver-container) - NVIDIA Driver for data center GPUs
  - [nvidia-container-toolkit](https://github.com/NVIDIA/nvidia-container-toolkit) - Container Toolkit and runtime
  - [dcgm-exporter](https://github.com/NVIDIA/dcgm-exporter) - DCGM monitoring and telemetry
  - Plus E-MOBI-specific AI and robotics optimizations

### E-MOBI Enhancement Areas

1. **AI Optimization** - Enhanced ML workload scheduling
2. **Robotics Support** - Multi-agent coordination on edge
3. **Autonomous Systems** - Real-time GPU management
4. **Scalability** - Improved resource orchestration
5. **Security** - Enhanced isolation and monitoring

---

## License

The E-MOBI GPU Operator respects and maintains the original Apache 2.0 license of the NVIDIA GPU Operator. All E-MOBI enhancements are also provided under Apache 2.0.

**License**: Apache License 2.0  
**Original Source**: https://github.com/NVIDIA/gpu-operator  
**E-MOBI Fork**: https://github.com/ekonomikmobil/E-MOBI-Robotics-Developpement-gpu-operator

---

## Contributions

E-MOBI is willing to work with partners and the community for adding platform support, enhancements, and features to the GPU Operator.

The E-MOBI GPU Operator is open-source and permissively licensed under Apache 2.0 with minimal requirements.

### How to Contribute

**To file feature requests, bugs, or questions**:
- Submit an issue at https://github.com/ekonomikmobil/E-MOBI-Robotics-Developpement-gpu-operator/issues

**To contribute code**:
- File a Pull Request at https://github.com/ekonomikmobil/E-MOBI-Robotics-Developpement-gpu-operator/pulls
- Contributions do not require explicit contributor license agreements (CLA)
- We expect contributors to certify that they have the right to submit their work under the Apache 2.0 license

### Contribution Guidelines

1. **Fork the repository** and create a feature branch
2. **Make your changes** with clear commit messages
3. **Sign your commits** using the Developer Certificate of Origin (see below)
4. **Test thoroughly** before submitting
5. **Submit a PR** with a clear description of changes
6. **Engage with reviewers** during the review process

---

## <a name="signing"></a>Signing Your Work

We require all contributors to sign their work using the Developer Certificate of Origin (DCO). This is a simple way to certify that you have the right to submit your work.

### Developer Certificate of Origin

```
Developer Certificate of Origin
Version 1.1

Copyright (C) 2004, 2006 The Linux Foundation and its contributors.
1 Letterman Drive
Suite D4700
San Francisco, CA, 94129

Everyone is permitted to copy and distribute verbatim copies of
this license document, but changing it is not allowed.

Developer's Certificate of Origin 1.1

By making a contribution to this project, I certify that:

(a) The contribution was created in whole or in part by me and I
    have the right to submit it under the open source license
    indicated in the file; or

(b) The contribution is based upon previous work that, to the best
    of my knowledge, is covered by an appropriate open source
    license and I have the right under that license to submit that
    work with modifications created in whole or in part by me, under
    the same open source license (unless I am permitted to submit
    under a different license), as indicated in the file; or

(c) The contribution was provided directly to me by some other
    person who certified (a), (b) or (c) and I have not modified it.

(d) I understand and agree that this project and the contribution
    are public and that a record of the contribution (including all
    personal information I submit with it, including my sign-off) is
    maintained indefinitely and may be redistributed consistent with
    this project or the open source license(s) involved.
```

### How to Sign Your Commits

Add a line to every git commit message:

```
Signed-off-by: Your Name <your.email@example.com>
```

Use your real name (sorry, no pseudonyms or anonymous contributions).

If you set your git config, you can sign commits automatically:

```bash
git config user.name "Your Name"
git config user.email "your.email@example.com"
git commit -s  # The -s flag signs the commit
```

---

## E-MOBI Community

E-MOBI values the contributions and feedback from the global community. We are committed to:

- **Openness**: Transparent development and decision-making
- **Inclusivity**: Welcoming contributors from all backgrounds
- **Quality**: Maintaining high standards for code and documentation
- **Respect**: Treating all community members with respect and professionalism
- **Sustainability**: Ensuring long-term viability and maintenance

---

## Support

If you have questions about contributing, please:

- Check existing issues and PRs for similar topics
- Open a new discussion or issue with clear details
- Contact the E-MOBI team at [info@emobi.tech](mailto:info@emobi.tech)

---

## Attribution

Thank you for contributing to E-MOBI GPU Operator! Your contributions help us build better AI-powered solutions for the future.

**E-MOBI / EKONOMIK MOBIL, S.R.L**  
*The Company of the Future is in Your Midst*

Leadership: Junior Jules (PDG)  
Contact: [Talently Profile](https://talently.tech/ly/j-jules)
