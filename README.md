[![license](https://img.shields.io/github/license/ekonomikmobil/E-MOBI-Robotics-Developpement-gpu-operator?style=flat-square)](https://raw.githubusercontent.com/ekonomikmobil/E-MOBI-Robotics-Developpement-gpu-operator/emobi-main/LICENSE)
[![E-MOBI Status](https://img.shields.io/badge/status-ACTIVE-brightgreen?style=flat-square)](https://talently.tech/ly/j-jules)

# E-MOBI / EKONOMIK MOBIL, S.R.L - GPU Operator
## E-MOBI Robotics Développement
### The Next Way...

![e-mobi-robotics](https://img.shields.io/badge/E--MOBI%20Robotics-AI%20Powered-blue?style=for-the-badge)

## Overview

E-MOBI / EKONOMIK MOBIL, S.R.L. through E-MOBI Robotics Développement, presents an advanced GPU Operator solution optimized for artificial intelligence, autonomous systems, and high-performance computing.

Kubernetes provides access to special hardware resources such as GPUs, NICs, Infiniband adapters and other devices through the [device plugin framework](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/device-plugins/). However, configuring and managing nodes with these hardware resources requires configuration of multiple software components such as drivers, container runtimes or other libraries which are difficult and prone to errors.

The E-MOBI GPU Operator uses the [operator framework](https://cloud.redhat.com/blog/introducing-the-operator-framework) within Kubernetes to automate the management of all GPU software components needed to provision accelerated computing resources. These components include the NVIDIA drivers (to enable CUDA), Kubernetes device plugin for GPUs, the NVIDIA Container Runtime, automatic node labelling, [DCGM](https://developer.nvidia.com/dcgm) based monitoring and others.

---

## 🎯 E-MOBI Core Values

**Autonomous** • **Useful** • **Efficient** • **Innovative** • **Secure** • **Responsible** • **Self-sufficient** • **Sovereign** • **Evolutionary**

### E-MOBI Robotics Développement - Mission Pillars

#### 1. **Revolutionary Innovations**
We are at the forefront of the latest advances in AI, developing innovative solutions that redefine industry standards. From fundamental research to practical application, our goal is to offer you a decisive competitive advantage.

#### 2. **Profound Transformations**
AI is a catalyst for change. We help companies achieve significant transformations by rethinking their processes, strategies, and business models to fully embrace the digital age.

#### 3. **Limitless Scalability**
Our solutions are designed to grow with you. Thanks to modular and flexible architectures, our AI systems adapt and evolve with your changing needs and business expansion.

#### 4. **Increased Productivity**
By automating repetitive tasks and optimizing workflows, our AI solutions unleash human potential, allowing your teams to focus on higher-value initiatives and achieve unprecedented levels of productivity.

#### 5. **Intelligent Automation**
We implement sophisticated and intelligent automation systems, enabling autonomous and optimized execution of operations, from data management to decision-making.

#### 6. **Operational Efficiencies**
AI is a powerful lever for optimization. We identify bottlenecks and design algorithms that streamline your operations, reduce costs, and maximize the use of your resources.

#### 7. **Guaranteed Sustainability**
Our approaches incorporate a long-term vision. By designing robust and sustainable solutions, we ensure the resilience of your systems and contribute to sustainable and responsible growth.

#### 8. **Concrete Benefits**
Each AI solution we offer is designed to deliver tangible added value. From improving the customer experience to optimizing the supply chain, our applications have a direct and measurable impact on your bottom line.

#### 9. **Essential Self-Sustainability**
Our goal is to equip you to master and fully leverage the potential of AI. We transfer the knowledge and skills necessary for you to become autonomous in the management and evolution of your intelligent systems.

#### 10. **Continuous Security**
The security of your data and systems is our top priority. We integrate the most advanced security protocols into every step of our development, ensuring consistent protection and unwavering confidence in your AI-powered operations.

---

## Audience and Use-Cases

The E-MOBI GPU Operator allows administrators of Kubernetes clusters to manage GPU nodes just like CPU nodes in the cluster. Instead of provisioning a special OS image for GPU nodes, administrators can rely on a standard OS image for both CPU and GPU nodes and then rely on the GPU Operator to provision the required software components for GPUs.

Note that the GPU Operator is specifically useful for scenarios where the Kubernetes cluster needs to scale quickly - for example provisioning additional GPU nodes on the cloud or on-prem and managing them.

---

## Quick Start

This section provides a quick guide for deploying the E-MOBI GPU Operator.

Make sure your Kubernetes cluster meets the [prerequisites](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/latest/getting-started.html#prerequisites).

**Step 1: Add the E-MOBI Helm repository**

```bash
helm repo add emobi https://helm.ekonomikmobil.com/emobi \
    && helm repo update
```

**Step 2: Deploy E-MOBI GPU Operator**

```bash
helm install --wait --generate-name \
    -n gpu-operator --create-namespace \
    emobi/gpu-operator
```

After installation, the E-MOBI GPU Operator and its operands should be up and running.

---

## Product Documentation

For information on platform support and getting started, visit the official documentation at [E-MOBI Knowledge Base](https://emobi.tech).

---

## Roadmap

- Support the latest Data Center GPUs, systems, and drivers
- Enhanced AI/ML optimization for autonomous systems
- Support RHEL 10 and beyond
- Support KubeVirt with Ubuntu 24.04+
- Promote advanced GPU driver configurations to General Availability (GA)
- Integrate advanced DRA Driver for GPUs as a managed component
- Edge AI deployment optimization
- Robotics-specific optimizations
- Multi-GPU orchestration enhancements

---

## Webinar & Resources

- [How to use GPUs on Kubernetes](https://info.nvidia.com/how-to-use-gpus-on-kubernetes-webinar.html)
- [E-MOBI AI Solutions](https://talently.tech/ly/j-jules)

---

## Contributions

E-MOBI welcomes contributions from the community. [Read the document on contributions](./CONTRIBUTING.md). You can contribute by opening a [pull request](https://help.github.com/en/articles/about-pull-requests).

---

## Support and Getting Help

For support, feature requests, or questions:
- Open [an issue on GitHub](https://github.com/ekonomikmobil/E-MOBI-Robotics-Developpement-gpu-operator/issues/new)
- Contact E-MOBI Robotics Développement: [info@emobi.tech](mailto:info@emobi.tech)
- Visit [E-MOBI Official](https://talently.tech/ly/j-jules)

Your feedback and contributions are appreciated.

---

## License

This project is licensed under the Apache License 2.0. See the [LICENSE](./LICENSE) file for details.

**Original Project**: NVIDIA GPU Operator  
**E-MOBI Customization**: E-MOBI / EKONOMIK MOBIL, S.R.L  
**Sovereignty**: Maintained independently with respect to open-source principles

---

## About E-MOBI / EKONOMIK MOBIL, S.R.L

**E-MOBI / EKONOMIK MOBIL, S.R.L** - The Company of the Future

E-MOBI Robotics Développement is a specialized division dedicated to:
- Advanced AI and machine learning solutions
- Autonomous systems and robotics
- GPU-accelerated computing
- Cloud-native technologies
- Enterprise AI infrastructure

**Leadership**: Junior Jules (PDG)  
**Contact**: [Talently Profile](https://talently.tech/ly/j-jules)

---

*Together, let's build a smarter, more efficient, and more secure future for your business.*
